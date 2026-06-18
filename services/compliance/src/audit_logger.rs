use sha2::{Sha256, Digest};
use std::fs::{File, OpenOptions};
use std::io::Write;

#[derive(Debug, Clone)]
pub struct AuditRecord {
    pub timestamp_ns: u64,
    pub order_id: u64,
    pub action: &'static str,
    pub price: u32,
    pub qty: u32,
    pub client_id: u32,
    pub instrument_id: u32,
}

pub struct AuditLogger {
    log_path: String,
    current_state_hash: [u8; 32],
    records_written: u64,
}

impl AuditLogger {
    pub fn new(log_path: &str) -> Self {
        Self {
            log_path: log_path.to_string(),
            current_state_hash: [0u8; 32],
            records_written: 0,
        }
    }

    pub fn log_transaction(&mut self, record: &AuditRecord) -> std::io::Result<()> {
        let serialized = format!(
            "TS:{}|ID:{}|ACT:{}|P:{}|Q:{}|CL:{}|INST:{}\n",
            record.timestamp_ns, record.order_id, record.action,
            record.price, record.qty, record.client_id, record.instrument_id
        );

        let mut hasher = Sha256::new();
        hasher.update(&self.current_state_hash);
        hasher.update(serialized.as_bytes());
        self.current_state_hash.copy_from_slice(&hasher.finalize());

        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.log_path)?;

        writeln!(file, "{}HASH:{}", serialized.trim(), hex::encode(self.current_state_hash))?;

        self.records_written += 1;

        if self.records_written % 10000 == 0 {
            println!("[AUDIT] {} records written. Chain hash: {}",
                     self.records_written, self.get_chain_hash());
        }

        Ok(())
    }

    pub fn get_chain_hash(&self) -> String {
        hex::encode(self.current_state_hash)
    }

    pub fn verify_chain(file_path: &str) -> std::io::Result<bool> {
        let content = std::fs::read_to_string(file_path)?;
        let mut prev_hash = [0u8; 32];

        for line in content.lines() {
            if let Some(hash_str) = line.split("HASH:").nth(1) {
                let hash_bytes = hex::decode(hash_str.trim())
                    .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;

                if hash_bytes != prev_hash {
                    return Ok(false);
                }

                let record_part = line.split("HASH:").next().unwrap_or("");
                let mut hasher = Sha256::new();
                hasher.update(&prev_hash);
                hasher.update(record_part.as_bytes());
                let computed = hasher.finalize();

                if computed.as_slice() != hash_bytes {
                    return Ok(false);
                }
                prev_hash.copy_from_slice(&hash_bytes);
            }
        }

        Ok(true)
    }

    pub fn record_count(&self) -> u64 {
        self.records_written
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_audit_logging() {
        let path = "/tmp/robin_audit_test.log";
        let mut logger = AuditLogger::new(path);

        let record = AuditRecord {
            timestamp_ns: 1234567890,
            order_id: 1,
            action: "NEW",
            price: 50000,
            qty: 100,
            client_id: 42,
            instrument_id: 100,
        };

        assert!(logger.log_transaction(&record).is_ok());
        assert_eq!(logger.record_count(), 1);
        assert!(!logger.get_chain_hash().is_empty());

        assert!(AuditLogger::verify_chain(path).unwrap_or(false));
        fs::remove_file(path).ok();
    }
}
