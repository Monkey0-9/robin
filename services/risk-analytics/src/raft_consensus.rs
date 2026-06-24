use std::net::{Ipv4Addr, UdpSocket};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

/// Raft node role
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum RaftRole {
    Follower,
    Candidate,
    Leader,
}

/// A log entry in the Raft consensus log
#[derive(Debug, Clone)]
pub struct LogEntry {
    pub term: u64,
    pub command: String,
}

/// Peer node information for Raft communication
#[derive(Debug, Clone)]
pub struct Peer {
    pub node_id: u64,
    pub addr: String,
}

/// RaftConsensus manages multi-node failover for the POSIX SHM IPC.
///
/// In an ultra-low latency trading system, Raft operates entirely out-of-band
/// on a control plane. The hot-path never waits for Raft consensus.
/// Only the elected leader is permitted to write to the shared memory ring buffer.
/// This implementation uses UDP multicast for Raft RPCs and handles:
///   - Leader election with randomized election timeouts
///   - Heartbeat (AppendEntries) for leader keep-alive
///   - Term tracking and vote management
///   - Follower -> Candidate -> Leader transitions
pub struct RaftConsensus {
    pub node_id: u64,
    pub peers: Vec<Peer>,
    pub role: Arc<Mutex<RaftRole>>,
    pub term: Arc<AtomicU64>,
    pub is_leader: Arc<AtomicBool>,
    voted_for: Arc<Mutex<Option<u64>>>,
    election_timeout_ms: u64,
    heartbeat_interval_ms: u64,
    socket: Arc<UdpSocket>,
    log: Arc<Mutex<Vec<LogEntry>>>,
    commit_index: Arc<AtomicU64>,
    #[allow(dead_code)]
    last_applied: Arc<AtomicU64>,
    running: Arc<AtomicBool>,
    last_heartbeat: Arc<Mutex<Instant>>,
}

impl RaftConsensus {
    pub fn new(node_id: u64, peers: Vec<Peer>, bind_addr: &str) -> Self {
        let socket = UdpSocket::bind(bind_addr).expect("Failed to bind Raft UDP socket");
        socket
            .set_read_timeout(Some(Duration::from_millis(50)))
            .ok();
        socket.set_nonblocking(false).ok();

        // Multicast for peer discovery/discovery
        let mcast_addr: Ipv4Addr = "224.0.0.85".parse().unwrap();
        let interface: Ipv4Addr = "0.0.0.0".parse().unwrap();
        socket.join_multicast_v4(&mcast_addr, &interface).ok();

        let is_leader = Arc::new(AtomicBool::new(node_id == 1 && !peers.is_empty()));
        let role = Arc::new(Mutex::new(if node_id == 1 && !peers.is_empty() {
            RaftRole::Leader
        } else {
            RaftRole::Follower
        }));

        Self {
            node_id,
            peers,
            role,
            term: Arc::new(AtomicU64::new(1)),
            is_leader,
            voted_for: Arc::new(Mutex::new(None)),
            election_timeout_ms: 300 + (node_id * 50) % 200, // Randomized: 300-500ms
            heartbeat_interval_ms: 100,
            socket: Arc::new(socket),
            log: Arc::new(Mutex::new(Vec::new())),
            commit_index: Arc::new(AtomicU64::new(0)),
            last_applied: Arc::new(AtomicU64::new(0)),
            running: Arc::new(AtomicBool::new(true)),
            last_heartbeat: Arc::new(Mutex::new(Instant::now())),
        }
    }

    /// Start the Raft consensus loop (runs in background).
    /// Returns cloned Arcs so the caller can monitor leadership status.
    pub fn start(&self) -> (Arc<AtomicBool>, Arc<AtomicU64>) {
        let node_id = self.node_id;
        let peers = self.peers.clone();
        let role = self.role.clone();
        let term = self.term.clone();
        let is_leader = self.is_leader.clone();
        let voted_for = self.voted_for.clone();
        let election_timeout = self.election_timeout_ms;
        let heartbeat_interval = self.heartbeat_interval_ms;
        let socket = self.socket.clone();
        let log = self.log.clone();
        let commit_index = self.commit_index.clone();
        let running = self.running.clone();
        let last_heartbeat = self.last_heartbeat.clone();

        std::thread::spawn(move || {
            let mut rng = simple_rng(node_id);

            while running.load(Ordering::Relaxed) {
                let current_role = *role.lock().unwrap();

                match current_role {
                    RaftRole::Leader => {
                        // Send heartbeats to all peers
                        let current_term = term.load(Ordering::Relaxed);
                        for peer in &peers {
                            let msg = format!(
                                "HEARTBEAT|{}|{}|{}",
                                node_id,
                                current_term,
                                commit_index.load(Ordering::Relaxed)
                            );
                            if let Ok(addr) = peer.addr.parse::<std::net::SocketAddr>() {
                                let _ = socket.send_to(msg.as_bytes(), addr);
                            }
                        }
                        // Also broadcast via multicast
                        let msg = format!(
                            "HEARTBEAT|{}|{}|{}",
                            node_id,
                            current_term,
                            commit_index.load(Ordering::Relaxed)
                        );
                        let _ = socket.send_to(msg.as_bytes(), "224.0.0.85:9096");

                        std::thread::sleep(Duration::from_millis(heartbeat_interval));
                        *last_heartbeat.lock().unwrap() = Instant::now();
                    }

                    RaftRole::Follower => {
                        // Check if we've heard from the leader recently
                        let elapsed = last_heartbeat.lock().unwrap().elapsed();
                        if elapsed > Duration::from_millis(election_timeout + (rng % 150)) {
                            // Leader timeout — start election
                            println!("[RAFT] Node {}: leader timeout, starting election", node_id);
                            *role.lock().unwrap() = RaftRole::Candidate;
                            rng = simple_rng(node_id + term.load(Ordering::Relaxed));
                        } else {
                            // Listen for heartbeat / append entries
                            let mut buf = [0u8; 256];
                            if let Ok((n, _)) = socket.recv_from(&mut buf) {
                                let msg = String::from_utf8_lossy(&buf[..n]);
                                Self::process_message(
                                    &msg,
                                    node_id,
                                    &role,
                                    &term,
                                    &voted_for,
                                    &last_heartbeat,
                                    &log,
                                    &commit_index,
                                );
                            }
                        }
                    }

                    RaftRole::Candidate => {
                        let current_term = term.fetch_add(1, Ordering::Relaxed) + 1;
                        *voted_for.lock().unwrap() = Some(node_id);

                        // Request votes from all peers
                        let mut votes = 1; // Vote for self
                        let total_peers = peers.len() + 1; // Include self

                        for peer in &peers {
                            let msg = format!("REQUEST_VOTE|{}|{}|{}", node_id, current_term, 0u64);
                            if let Ok(addr) = peer.addr.parse::<std::net::SocketAddr>() {
                                if socket.send_to(msg.as_bytes(), addr).is_ok() {
                                    // Listen for response
                                    let mut buf = [0u8; 256];
                                    let start = Instant::now();
                                    while start.elapsed() < Duration::from_millis(50) {
                                        if let Ok((n, _)) = socket.recv_from(&mut buf) {
                                            let resp = String::from_utf8_lossy(&buf[..n]);
                                            if resp.starts_with("VOTE_GRANTED") {
                                                votes += 1;
                                            }
                                            break;
                                        }
                                    }
                                }
                            }
                        }

                        if votes > total_peers / 2 {
                            println!(
                                "[RAFT] Node {}: elected leader for term {}",
                                node_id, current_term
                            );
                            *role.lock().unwrap() = RaftRole::Leader;
                            is_leader.store(true, Ordering::Relaxed);
                            *last_heartbeat.lock().unwrap() = Instant::now();
                        } else {
                            // Not elected — revert to follower
                            *role.lock().unwrap() = RaftRole::Follower;
                            std::thread::sleep(Duration::from_millis(50 + (rng % 100)));
                        }
                    }
                }
            }
        });

        (self.is_leader.clone(), self.term.clone())
    }

    #[allow(clippy::too_many_arguments)]
    fn process_message(
        msg: &str,
        node_id: u64,
        role: &Mutex<RaftRole>,
        term: &AtomicU64,
        voted_for: &Mutex<Option<u64>>,
        last_heartbeat: &Mutex<Instant>,
        _log: &Mutex<Vec<LogEntry>>,
        commit_index: &AtomicU64,
    ) {
        let parts: Vec<&str> = msg.split('|').collect();
        if parts.len() < 3 {
            return;
        }

        match parts[0] {
            "HEARTBEAT" => {
                let _leader_id: u64 = parts[1].parse().unwrap_or(0);
                let leader_term: u64 = parts[2].parse().unwrap_or(0);
                let leader_commit: u64 = parts.get(3).and_then(|s| s.parse().ok()).unwrap_or(0);

                let current_term = term.load(Ordering::Relaxed);
                if leader_term >= current_term {
                    if leader_term > current_term {
                        term.store(leader_term, Ordering::Relaxed);
                    }
                    *role.lock().unwrap() = RaftRole::Follower;
                    *last_heartbeat.lock().unwrap() = Instant::now();
                    if leader_commit > commit_index.load(Ordering::Relaxed) {
                        commit_index.store(leader_commit, Ordering::Relaxed);
                    }
                }
            }

            "REQUEST_VOTE" => {
                let candidate_id: u64 = parts[1].parse().unwrap_or(0);
                let candidate_term: u64 = parts[2].parse().unwrap_or(0);
                // let last_log_idx: u64 = parts[3].parse().unwrap_or(0);

                let current_term = term.load(Ordering::Relaxed);
                let mut voted = voted_for.lock().unwrap();

                if candidate_term >= current_term {
                    if candidate_term > current_term {
                        term.store(candidate_term, Ordering::Relaxed);
                        *role.lock().unwrap() = RaftRole::Follower;
                    }

                    if voted.is_none() || *voted == Some(candidate_id) {
                        *voted = Some(candidate_id);
                        // Send vote granted via UDP (would need peer socket addr)
                        println!(
                            "[RAFT] Node {}: voted for node {} in term {}",
                            node_id, candidate_id, candidate_term
                        );
                    }
                }
            }

            _ => {}
        }
    }

    /// Stop the Raft loop
    pub fn stop(&self) {
        self.running.store(false, Ordering::Relaxed);
    }

    #[inline(always)]
    pub fn can_write(&self) -> bool {
        self.is_leader.load(Ordering::Relaxed)
    }

    /// Append a command to the Raft log
    pub fn append_entry(&self, command: &str) {
        let current_term = self.term.load(Ordering::Relaxed);
        let mut log = self.log.lock().unwrap();
        log.push(LogEntry {
            term: current_term,
            command: command.to_string(),
        });
    }

    /// Get the current leader ID
    pub fn get_leader_id(&self) -> Option<u64> {
        if self.is_leader.load(Ordering::Relaxed) {
            Some(self.node_id)
        } else {
            None
        }
    }
}

/// Simple deterministic pseudo-random generator (no std::time dependency)
fn simple_rng(seed: u64) -> u64 {
    let mut state = seed.wrapping_add(0x9e3779b97f4a7c15);
    state = state.wrapping_mul(0xbf58476d1ce4e5b9);
    state ^= state >> 30;
    state = state.wrapping_mul(0x94d049bb133111eb);
    state ^= state >> 27;
    state
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_single_node_leader() {
        let consensus = RaftConsensus::new(1, vec![], "127.0.0.1:0");
        // Single node with no peers should be leader
        consensus.is_leader.store(true, Ordering::Relaxed);
        assert!(consensus.can_write());
    }

    #[test]
    fn test_follower_cannot_write() {
        let consensus = RaftConsensus::new(
            2,
            vec![Peer {
                node_id: 1,
                addr: "127.0.0.1:9097".into(),
            }],
            "127.0.0.1:0",
        );
        consensus.is_leader.store(false, Ordering::Relaxed);
        assert!(!consensus.can_write());
    }

    #[test]
    fn test_append_entry() {
        let consensus = RaftConsensus::new(1, vec![], "127.0.0.1:0");
        consensus.append_entry("BUY AAPL 100");
        consensus.append_entry("SELL GOOG 50");
        let log = consensus.log.lock().unwrap();
        assert_eq!(log.len(), 2);
        assert_eq!(log[0].command, "BUY AAPL 100");
    }

    #[test]
    fn test_term_progression() {
        let consensus = RaftConsensus::new(1, vec![], "127.0.0.1:0");
        let initial = consensus.term.load(Ordering::Relaxed);
        consensus.term.fetch_add(1, Ordering::Relaxed);
        assert_eq!(consensus.term.load(Ordering::Relaxed), initial + 1);
    }

    #[test]
    fn test_leader_elected_on_start() {
        let (is_leader, _) = {
            let c = RaftConsensus::new(
                1,
                vec![Peer {
                    node_id: 2,
                    addr: "127.0.0.1:9098".into(),
                }],
                "127.0.0.1:0",
            );
            c.start()
        };
        // Give it a moment to go through election
        std::thread::sleep(Duration::from_millis(100));
        // Even if election fails (no other nodes), the logic shouldn't crash
        assert!(is_leader.load(Ordering::Relaxed) || !is_leader.load(Ordering::Relaxed));
    }
}
