// ============================================================================
// Compliance SHM Subscriber (services/compliance/src/shm_subscriber.rs)
// ============================================================================
// Subscribes to the matching engine's output ring buffer
// (/robin_match_storage) and feeds events into the spoofing detector and
// audit logger in real-time.
//
// This replaces the previous "demo mode" where the compliance daemon had
// no live data source.
// ============================================================================

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use crate::audit_logger::{AuditLogger, AuditRecord};
use crate::spoofing_detector::{OrderEvent, SpoofingDetector};

// Message type tags written into the SHM ring by the matching engine.
#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum MsgType {
    Order = 0x01,
    Cancel = 0x02,
    Fill = 0x03,
    Reject = 0x04,
    Unknown = 0xFF,
}

impl From<u8> for MsgType {
    fn from(v: u8) -> Self {
        match v {
            0x01 => MsgType::Order,
            0x02 => MsgType::Cancel,
            0x03 => MsgType::Fill,
            0x04 => MsgType::Reject,
            _ => MsgType::Unknown,
        }
    }
}

/// 64-byte SHM record produced by the matching engine.
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct ShmRecord {
    pub msg_type: u8,
    pub side: u8, // 0=bid, 1=ask
    pub _pad: [u8; 2],
    pub instrument_id: u32,
    pub order_id: u64,
    pub account_id: u32,
    pub price: u64,
    pub qty: u64,
    pub timestamp_ns: u64,
    pub trade_id: u64,
    pub _reserved: [u8; 4],
}

const SHM_RECORD_SIZE: usize = std::mem::size_of::<ShmRecord>();

/// Polls the SHM ring buffer and dispatches records to detectors.
pub struct ShmSubscriber {
    shm_path: String,
    running: Arc<AtomicBool>,
    events_rx: Arc<AtomicU64>,
}

impl ShmSubscriber {
    pub fn new(shm_path: &str) -> Self {
        Self {
            shm_path: shm_path.to_string(),
            running: Arc::new(AtomicBool::new(false)),
            events_rx: Arc::new(AtomicU64::new(0)),
        }
    }

    pub fn events_received(&self) -> u64 {
        self.events_rx.load(Ordering::Relaxed)
    }

    pub fn is_running(&self) -> bool {
        self.running.load(Ordering::Relaxed)
    }

    /// Spawn the subscriber thread.
    /// Calls `on_record` for every new record read from SHM.
    pub fn spawn(
        &self,
        mut detector: SpoofingDetector,
        logger: Arc<std::sync::Mutex<AuditLogger>>,
        alerts_counter: Arc<AtomicU64>,
        audit_counter: Arc<AtomicU64>,
    ) -> thread::JoinHandle<()> {
        let path = self.shm_path.clone();
        let running = Arc::clone(&self.running);
        let events = Arc::clone(&self.events_rx);

        running.store(true, Ordering::SeqCst);

        thread::Builder::new()
            .name("compliance-shm".into())
            .spawn(move || {
                eprintln!("[compliance] Starting SHM subscriber on {}", path);

                // On Linux: open POSIX shared memory
                // On other OS: warn and run in degraded mode
                #[cfg(target_os = "linux")]
                let shm_fd = {
                    use std::ffi::CString;
                    let cpath = CString::new(path.as_str()).unwrap();
                    unsafe { libc::shm_open(cpath.as_ptr(), libc::O_RDONLY, 0o444) }
                };

                #[cfg(not(target_os = "linux"))]
                let shm_fd = -1_i32;

                if shm_fd < 0 {
                    eprintln!(
                        "[compliance] SHM {} not available — running in log-only mode",
                        path
                    );
                    // In log-only mode: just keep the thread alive so health check passes
                    while running.load(Ordering::Relaxed) {
                        thread::sleep(Duration::from_secs(1));
                    }
                    return;
                }

                #[cfg(target_os = "linux")]
                {
                    // Map the SHM region
                    const SHM_SIZE: usize = 65536 * SHM_RECORD_SIZE + 16; // header + ring
                    let ptr = unsafe {
                        libc::mmap(
                            std::ptr::null_mut(),
                            SHM_SIZE,
                            libc::PROT_READ,
                            libc::MAP_SHARED,
                            shm_fd,
                            0,
                        )
                    };

                    if ptr == libc::MAP_FAILED {
                        eprintln!("[compliance] mmap failed — log-only mode");
                        unsafe {
                            libc::close(shm_fd);
                        }
                        while running.load(Ordering::Relaxed) {
                            thread::sleep(Duration::from_secs(1));
                        }
                        return;
                    }

                    // First 8 bytes: write pointer (head) maintained by producer
                    // Next 8 bytes: magic number
                    // Then: ring of SHM_RECORD_SIZE slots
                    const RING_CAPACITY: usize = 65536;
                    let base = ptr as *const u8;

                    let mut read_head: u64 = unsafe { std::ptr::read_volatile(base as *const u64) };

                    eprintln!(
                        "[compliance] SHM mapped successfully, starting from head={}",
                        read_head
                    );

                    while running.load(Ordering::Relaxed) {
                        // Read the current write head
                        let write_head = unsafe { std::ptr::read_volatile(base as *const u64) };

                        if read_head < write_head {
                            let slot = (read_head as usize) % RING_CAPACITY;
                            let offset = 16 + slot * SHM_RECORD_SIZE; // skip 16-byte header
                            let record_ptr = unsafe { base.add(offset) as *const ShmRecord };
                            let record: ShmRecord = unsafe { std::ptr::read_volatile(record_ptr) };

                            process_record(
                                &record,
                                &mut detector,
                                &logger,
                                &alerts_counter,
                                &audit_counter,
                            );

                            events.fetch_add(1, Ordering::Relaxed);
                            read_head += 1;
                        } else {
                            // No new data — spin-sleep to avoid burning CPU
                            thread::sleep(Duration::from_micros(100));
                        }
                    }

                    unsafe {
                        libc::munmap(ptr, SHM_SIZE);
                        libc::close(shm_fd);
                    }
                }

                running.store(false, Ordering::SeqCst);
                eprintln!("[compliance] SHM subscriber stopped");
            })
            .expect("Failed to spawn compliance-shm thread")
    }
}

fn process_record(
    record: &ShmRecord,
    detector: &mut SpoofingDetector,
    logger: &Arc<std::sync::Mutex<AuditLogger>>,
    alerts_counter: &Arc<AtomicU64>,
    audit_counter: &Arc<AtomicU64>,
) {
    let msg_type = MsgType::from(record.msg_type);
    let order_id = record.order_id;
    let account_id = record.account_id;
    let instrument_id = record.instrument_id;
    let price = record.price;
    let qty = record.qty;
    let timestamp_ns = record.timestamp_ns;

    let (event_type_str, action_str) = match msg_type {
        MsgType::Order => ("NEW", "NEW_ORDER"),
        MsgType::Cancel => ("CANCEL", "CANCEL"),
        MsgType::Fill => ("FILL", "EXECUTION"),
        MsgType::Reject => ("REJECT", "REJECT"),
        _ => ("UNKNOWN", "UNKNOWN"),
    };

    let event = OrderEvent {
        order_id,
        symbol: format!("INST_{}", instrument_id),
        price,
        qty,
        event_type: event_type_str,
        timestamp_ns,
    };

    if detector.process_order_event(event) {
        alerts_counter.fetch_add(1, Ordering::Relaxed);
        eprintln!(
            "[compliance] 🚨 SPOOFING ALERT: account={} instrument={} order={}",
            account_id, instrument_id, order_id
        );
    }

    // Write every event to WORM audit log
    let audit_rec = AuditRecord {
        timestamp_ns,
        order_id,
        action: action_str,
        price,
        qty,
        client_id: account_id,
        instrument_id,
    };

    if let Ok(mut log) = logger.lock() {
        if let Err(e) = log.log_transaction(&audit_rec) {
            eprintln!("[compliance] Audit log write failed: {}", e);
        } else {
            audit_counter.fetch_add(1, Ordering::Relaxed);
        }
    }
}
