use sha2::{Digest, Sha256};
use std::fs::OpenOptions;
use std::io::Write;

#[derive(Debug, Clone)]
pub struct AuditRecord {
    pub timestamp_ns: u64,
    pub order_id: u64,
    pub action: &'static str,
    pub price: u64,
    pub qty: u64,
    pub client_id: u32,
    pub instrument_id: u32,
}

pub struct AuditLogger {
    log_path: String,
    current_state_hash: [u8; 32],
    records_written: u64,
}

fn chain_hash(prev: &[u8; 32], record: &str) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(prev);
    hasher.update(record.as_bytes());
    hasher.finalize().into()
}

impl AuditLogger {
    pub fn new(log_path: &str) -> Self {
        // On restart the in-memory chain head MUST be re-derived from disk,
        // otherwise new records chain off a zero hash and the tamper-evident
        // chain silently breaks. Compute the head from the last persisted line.
        let current_state_hash = Self::last_chain_hash(log_path);
        Self {
            log_path: log_path.to_string(),
            current_state_hash,
            records_written: 0,
        }
    }

    /// Re-derive the last chain hash by scanning the persisted ledger. Empty
    /// file -> zero head (fresh chain).
    fn last_chain_hash(log_path: &str) -> [u8; 32] {
        let Ok(content) = std::fs::read_to_string(log_path) else {
            return [0u8; 32];
        };
        let mut prev = [0u8; 32];
        for line in content.lines() {
            let mut parts = line.splitn(2, "HASH:");
            let record_part = match parts.next() {
                Some(p) => p,
                None => continue,
            };
            let hash_hex = match parts.next() {
                Some(h) => h.trim(),
                None => continue,
            };
            if let Ok(hash_bytes) = hex::decode(hash_hex) {
                let expected = chain_hash(&prev, &format!("{}\n", record_part));
                if expected.as_slice() == hash_bytes.as_slice() {
                    prev.copy_from_slice(&hash_bytes);
                }
            }
        }
        prev
    }

    pub fn log_transaction(&mut self, record: &AuditRecord) -> std::io::Result<()> {
        let serialized = format!(
            "TS:{}|ID:{}|ACT:{}|P:{}|Q:{}|CL:{}|INST:{}\n",
            record.timestamp_ns,
            record.order_id,
            record.action,
            record.price,
            record.qty,
            record.client_id,
            record.instrument_id
        );

        let next_hash = chain_hash(&self.current_state_hash, &serialized);
        self.current_state_hash = next_hash;

        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.log_path)?;

        writeln!(
            file,
            "{}HASH:{}",
            serialized.trim(),
            hex::encode(self.current_state_hash)
        )?;

        // SEC 17a-4 durability: force the record to stable storage before any
        // downstream code observes a success, so a crash cannot lose or
        // reorder the append.
        file.sync_all()?;

        self.records_written += 1;

        #[allow(clippy::manual_is_multiple_of)]
        if self.records_written % 10000 == 0 {
            println!(
                "[AUDIT] {} records written. Chain hash: {}",
                self.records_written,
                self.get_chain_hash()
            );
        }

        Ok(())
    }

    pub fn get_chain_hash(&self) -> String {
        hex::encode(self.current_state_hash)
    }

    /// Recompute the whole chain from disk and confirm every link. This is the
    /// tamper check an operator triggers after any incident or before moving
    /// evidence off-box.
    pub fn verify_chain(file_path: &str) -> std::io::Result<bool> {
        let content = std::fs::read_to_string(file_path)?;
        let mut prev_hash = [0u8; 32];

        for line in content.lines() {
            if let Some(hash_str) = line.split("HASH:").nth(1) {
                let hash_bytes = hex::decode(hash_str.trim())
                    .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;

                let record_part = line.split("HASH:").next().unwrap_or("");
                let record_part_with_newline = format!("{}\n", record_part);
                let mut hasher = Sha256::new();
                hasher.update(prev_hash);
                hasher.update(record_part_with_newline.as_bytes());
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

    fn sample_record(order_id: u64, action: &'static str) -> AuditRecord {
        AuditRecord {
            timestamp_ns: 1_700_000_000_000,
            order_id,
            action,
            price: 50_000,
            qty: 100,
            client_id: 1,
            instrument_id: 2,
        }
    }

    fn temp_log_path(tag: &str) -> String {
        let dir = std::env::temp_dir();
        dir.join(format!(
            "robin_audit_test_{}_{}.log",
            tag,
            std::process::id()
        ))
        .to_string_lossy()
        .into_owned()
    }

    #[test]
    fn chain_survives_restart_and_resumes() {
        let path = temp_log_path("restart");
        let _ = std::fs::remove_file(&path);

        let mut logger = AuditLogger::new(&path);
        logger.log_transaction(&sample_record(1, "NEW")).unwrap();
        logger.log_transaction(&sample_record(2, "TRADE")).unwrap();
        let hash_at_shutdown = logger.get_chain_hash();

        // New AuditLogger instance (process restart) must NOT start from a
        // zero hash; it must re-derive the chain head from disk.
        let resumed = AuditLogger::new(&path);
        assert_eq!(resumed.get_chain_hash(), hash_at_shutdown);

        // Appending to the resumed chain must verify cleanly.
        let mut restarted = AuditLogger::new(&path);
        restarted
            .log_transaction(&sample_record(3, "CANCEL"))
            .unwrap();

        assert!(AuditLogger::verify_chain(&path).unwrap());

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn verify_chain_detects_tampering() {
        let path = temp_log_path("tamper");
        let _ = std::fs::remove_file(&path);

        let mut logger = AuditLogger::new(&path);
        logger.log_transaction(&sample_record(1, "NEW")).unwrap();
        logger.log_transaction(&sample_record(2, "TRADE")).unwrap();
        assert!(AuditLogger::verify_chain(&path).unwrap());

        // Mutating an existing record (Mid-record price change) breaks the
        // chain. The tampered price is embedded in the record body before the
        // hash delimiter, so both the record and the hash text change.
        let content = std::fs::read_to_string(&path).unwrap();
        let mutated = content.replace("P:50000", "P:99999");
        assert_ne!(content, mutated);
        std::fs::write(&path, mutated).unwrap();

        assert!(!AuditLogger::verify_chain(&path).unwrap());

        let _ = std::fs::remove_file(&path);
    }
}
