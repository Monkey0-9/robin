// ============================================================================
// Risk State Snapshot & Persistence Engine
// services/risk-analytics/src/persistence.rs
// ============================================================================
// Periodically snapshots positions, credit balances, P&L, and circuit breaker
// states with CRC-32 checksums and atomic rename guarantees.
// On startup, restores state from the latest validated snapshot.
// ============================================================================

use std::fs::{self, File};
use std::io::{self, Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};

const SNAPSHOT_MAGIC: u32 = 0x524F4253; // "ROBS"
const SNAPSHOT_VERSION: u16 = 1;

#[derive(Debug, Clone, PartialEq)]
pub struct AccountRiskState {
    pub account_id: u32,
    pub credit_limit: u64,
    pub used_credit: u64,
    pub realized_pnl: i64,
    pub unrealized_pnl: i64,
    pub position_qty: i64,
    pub circuit_breaker_tripped: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct RiskSnapshot {
    pub timestamp_ns: u64,
    pub sequence_num: u64,
    pub accounts: Vec<AccountRiskState>,
}

pub struct StatePersistence {
    storage_dir: PathBuf,
    last_snapshot_seq: AtomicU64,
    is_saving: AtomicBool,
}

impl StatePersistence {
    pub fn new(storage_dir: &str) -> io::Result<Self> {
        let dir = PathBuf::from(storage_dir);
        fs::create_dir_all(&dir)?;
        Ok(Self {
            storage_dir: dir,
            last_snapshot_seq: AtomicU64::new(0),
            is_saving: AtomicBool::new(false),
        })
    }

    /// Save snapshot atomically: write to temp file then rename
    pub fn save_snapshot(&self, snapshot: &RiskSnapshot) -> io::Result<PathBuf> {
        if self.is_saving.swap(true, Ordering::SeqCst) {
            return Err(io::Error::new(
                io::ErrorKind::WouldBlock,
                "Snapshot in progress",
            ));
        }

        let seq = snapshot.sequence_num;
        let filename = format!("risk_snapshot_{:012}.bin", seq);
        let tmp_filename = format!("risk_snapshot_{:012}.tmp", seq);

        let target_path = self.storage_dir.join(filename);
        let tmp_path = self.storage_dir.join(tmp_filename);

        let mut bytes = Vec::with_capacity(64 + snapshot.accounts.len() * 48);

        // Header
        bytes.extend_from_slice(&SNAPSHOT_MAGIC.to_le_bytes());
        bytes.extend_from_slice(&SNAPSHOT_VERSION.to_le_bytes());
        bytes.extend_from_slice(&(snapshot.accounts.len() as u32).to_le_bytes());
        bytes.extend_from_slice(&snapshot.timestamp_ns.to_le_bytes());
        bytes.extend_from_slice(&snapshot.sequence_num.to_le_bytes());

        // Account records
        for acc in &snapshot.accounts {
            bytes.extend_from_slice(&acc.account_id.to_le_bytes());
            bytes.extend_from_slice(&acc.credit_limit.to_le_bytes());
            bytes.extend_from_slice(&acc.used_credit.to_le_bytes());
            bytes.extend_from_slice(&acc.realized_pnl.to_le_bytes());
            bytes.extend_from_slice(&acc.unrealized_pnl.to_le_bytes());
            bytes.extend_from_slice(&acc.position_qty.to_le_bytes());
            bytes.push(if acc.circuit_breaker_tripped { 1 } else { 0 });
            bytes.extend_from_slice(&[0u8; 3]); // padding to 48 bytes per record
        }

        // CRC-32 checksum of data payload
        let crc = crc32_bitwise(&bytes);
        bytes.extend_from_slice(&crc.to_le_bytes());

        // Write to temporary file
        let mut file = File::create(&tmp_path)?;
        file.write_all(&bytes)?;
        file.sync_all()?;

        // Atomic replace
        fs::rename(&tmp_path, &target_path)?;

        self.last_snapshot_seq.store(seq, Ordering::Relaxed);
        self.is_saving.store(false, Ordering::SeqCst);

        Ok(target_path)
    }

    /// Load the most recent valid snapshot from disk
    pub fn load_latest_snapshot(&self) -> io::Result<Option<RiskSnapshot>> {
        let mut entries: Vec<_> = fs::read_dir(&self.storage_dir)?
            .filter_map(|e| e.ok())
            .filter(|e| {
                let name = e.file_name().to_string_lossy().to_string();
                name.starts_with("risk_snapshot_") && name.ends_with(".bin")
            })
            .collect();

        if entries.is_empty() {
            return Ok(None);
        }

        // Sort descending by sequence number in filename
        entries.sort_by_key(|e| e.file_name());
        entries.reverse();

        for entry in entries {
            let path = entry.path();
            if let Ok(snap) = Self::read_snapshot_file(&path) {
                return Ok(Some(snap));
            }
        }

        Ok(None)
    }

    fn read_snapshot_file(path: &Path) -> io::Result<RiskSnapshot> {
        let mut file = File::open(path)?;
        let mut bytes = Vec::new();
        file.read_to_end(&mut bytes)?;

        if bytes.len() < 30 {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Snapshot too small",
            ));
        }

        let magic = u32::from_le_bytes(bytes[0..4].try_into().unwrap());
        if magic != SNAPSHOT_MAGIC {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid magic number",
            ));
        }

        let total_data_len = bytes.len() - 4;
        let stored_crc = u32::from_le_bytes(bytes[total_data_len..].try_into().unwrap());
        let calc_crc = crc32_bitwise(&bytes[..total_data_len]);

        if stored_crc != calc_crc {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "CRC checksum mismatch",
            ));
        }

        let version = u16::from_le_bytes(bytes[4..6].try_into().unwrap());
        if version != SNAPSHOT_VERSION {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Unsupported version",
            ));
        }

        let count = u32::from_le_bytes(bytes[6..10].try_into().unwrap()) as usize;
        let timestamp_ns = u64::from_le_bytes(bytes[10..18].try_into().unwrap());
        let sequence_num = u64::from_le_bytes(bytes[18..26].try_into().unwrap());

        let mut offset = 26;
        let mut accounts = Vec::with_capacity(count);

        for _ in 0..count {
            if offset + 48 > total_data_len {
                return Err(io::Error::new(
                    io::ErrorKind::UnexpectedEof,
                    "Truncated record",
                ));
            }

            let account_id = u32::from_le_bytes(bytes[offset..offset + 4].try_into().unwrap());
            let credit_limit =
                u64::from_le_bytes(bytes[offset + 4..offset + 12].try_into().unwrap());
            let used_credit =
                u64::from_le_bytes(bytes[offset + 12..offset + 20].try_into().unwrap());
            let realized_pnl =
                i64::from_le_bytes(bytes[offset + 20..offset + 28].try_into().unwrap());
            let unrealized_pnl =
                i64::from_le_bytes(bytes[offset + 28..offset + 36].try_into().unwrap());
            let position_qty =
                i64::from_le_bytes(bytes[offset + 36..offset + 44].try_into().unwrap());
            let circuit_breaker_tripped = bytes[offset + 44] == 1;

            accounts.push(AccountRiskState {
                account_id,
                credit_limit,
                used_credit,
                realized_pnl,
                unrealized_pnl,
                position_qty,
                circuit_breaker_tripped,
            });

            offset += 48;
        }

        Ok(RiskSnapshot {
            timestamp_ns,
            sequence_num,
            accounts,
        })
    }
}

/// Bitwise reflected CRC-32
fn crc32_bitwise(data: &[u8]) -> u32 {
    let mut crc = 0xFFFFFFFFu32;
    for &byte in data {
        crc ^= byte as u32;
        for _ in 0..8 {
            crc = if (crc & 1) != 0 {
                (crc >> 1) ^ 0xEDB88320
            } else {
                crc >> 1
            };
        }
    }
    !crc
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::env;

    #[test]
    fn test_snapshot_roundtrip() {
        let tmp_dir = env::temp_dir().join("robin_snap_test");
        let persistence = StatePersistence::new(tmp_dir.to_str().unwrap()).unwrap();

        let original = RiskSnapshot {
            timestamp_ns: 1_700_000_000_000_000_000,
            sequence_num: 42,
            accounts: vec![
                AccountRiskState {
                    account_id: 101,
                    credit_limit: 10_000_000,
                    used_credit: 2_500_000,
                    realized_pnl: 15_000,
                    unrealized_pnl: -3_000,
                    position_qty: 500,
                    circuit_breaker_tripped: false,
                },
                AccountRiskState {
                    account_id: 102,
                    credit_limit: 5_000_000,
                    used_credit: 4_800_000,
                    realized_pnl: -80_000,
                    unrealized_pnl: -20_000,
                    position_qty: -200,
                    circuit_breaker_tripped: true,
                },
            ],
        };

        let path = persistence.save_snapshot(&original).unwrap();
        assert!(path.exists());

        let loaded = persistence
            .load_latest_snapshot()
            .unwrap()
            .expect("Snapshot exists");
        assert_eq!(loaded, original);

        // Cleanup
        let _ = fs::remove_dir_all(tmp_dir);
    }
}
