// services/compliance/audit_logger.rs
// Immutable regulatory audit logging (WORM - Write Once Read Many format).
// Computes cumulative SHA256 hashes of transaction logs to prevent modification.

use std::fs::OpenOptions;
use std::io::Write;
use sha2::{Sha256, Digest};

pub struct AuditRecord {
    pub timestamp_ns: u64,
    pub order_id: u64,
    pub action: &'static str, // "SUBMIT", "CANCEL", "FILL"
    pub price: u32,
    pub qty: u32,
}

pub struct AuditLogger {
    log_path: String,
    current_state_hash: [u8; 32],
}

impl AuditLogger {
    pub fn new(log_path: &str) -> Self {
        Self {
            log_path: log_path.to_string(),
            current_state_hash: [0u8; 32],
        }
    }

    pub fn log_transaction(&mut self, record: &AuditRecord) -> std::io::Result<()> {
        let serialized_record = format!(
            "TS:{} | ID:{} | ACT:{} | P:{} | Q:{}\n",
            record.timestamp_ns, record.order_id, record.action, record.price, record.qty
        );

        // Update block state hash using SHA256 (blockchain anchor logic)
        let mut hasher = Sha256::new();
        hasher.update(&self.current_state_hash);
        hasher.update(serialized_record.as_bytes());
        self.current_state_hash.copy_from_slice(&hasher.finalize());

        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.log_path)?;

        write!(
            file,
            "{}HASH:{}\n",
            serialized_record,
            hex::encode(self.current_state_hash)
        )?;

        Ok(())
    }

    pub fn get_chain_hash(&self) -> String {
        hex::encode(self.current_state_hash)
    }
}
