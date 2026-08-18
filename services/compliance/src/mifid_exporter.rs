// ============================================================================
// MiFID II Regulatory Reporting Exporter (RTS 22, RTS 25, Article 27)
// services/compliance/src/mifid_exporter.rs
// ============================================================================
// Generates European regulatory reports under MiFID II / MiFIR:
//   1. RTS 22: Transaction Reporting with LEI, Algo Flags, DEA, Buyer/Seller
//   2. RTS 25: Clock Synchronization & Max Divergence Evidence (UTC / PTP)
//   3. Article 27: Best Execution Quality & Venue Breakdown Reports
// ============================================================================

use std::fmt::Write as FmtWrite;
use std::fs;
use std::io::{self, Write};
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

/// RTS 22 Transaction Record
#[derive(Debug, Clone)]
pub struct MifidTransaction {
    pub transaction_ref_num: String,
    pub trading_venue_mic: String, // e.g. "XPAR", "XLON"
    pub buyer_lei: String,         // 20-char Legal Entity Identifier
    pub seller_lei: String,
    pub isin: String, // 12-char ISIN
    pub instrument_name: String,
    pub quantity: f64,
    pub price: f64,
    pub currency: String,
    pub trade_timestamp_ns: u64,
    pub direct_electronic_access: bool,
    pub algo_investment_decision: String, // Algo ID or "NONE"
    pub algo_execution: String,           // Algo ID
    pub waiver_flag: Option<String>,
}

impl MifidTransaction {
    pub fn to_xml(&self) -> String {
        let mut xml = String::new();
        let _ = write!(xml, "  <TxReport>\n");
        let _ = write!(
            xml,
            "    <TxRefNum>{}</TxRefNum>\n",
            self.transaction_ref_num
        );
        let _ = write!(xml, "    <VenueMIC>{}</VenueMIC>\n", self.trading_venue_mic);
        let _ = write!(xml, "    <BuyerLEI>{}</BuyerLEI>\n", self.buyer_lei);
        let _ = write!(xml, "    <SellerLEI>{}</SellerLEI>\n", self.seller_lei);
        let _ = write!(xml, "    <ISIN>{}</ISIN>\n", self.isin);
        let _ = write!(xml, "    <Qty>{:.4}</Qty>\n", self.quantity);
        let _ = write!(xml, "    <Price>{:.6}</Price>\n", self.price);
        let _ = write!(xml, "    <Ccy>{}</Ccy>\n", self.currency);
        let _ = write!(
            xml,
            "    <TimestampNS>{}</TimestampNS>\n",
            self.trade_timestamp_ns
        );
        let _ = write!(
            xml,
            "    <DEA>{}</DEA>\n",
            if self.direct_electronic_access {
                "true"
            } else {
                "false"
            }
        );
        let _ = write!(
            xml,
            "    <AlgoInvestDecision>{}</AlgoInvestDecision>\n",
            self.algo_investment_decision
        );
        let _ = write!(
            xml,
            "    <AlgoExecution>{}</AlgoExecution>\n",
            self.algo_execution
        );
        let _ = write!(xml, "  </TxReport>\n");
        xml
    }
}

/// RTS 25 Clock Synchronization Evidence
#[derive(Debug, Clone)]
pub struct ClockSyncRecord {
    pub timestamp_ns: u64,
    pub sync_source: String,            // "PTP_IEEE_1588_2008" or "GPS_PPS"
    pub offset_from_utc_ns: i64,        // divergence from UTC
    pub max_divergence_allowed_ns: u64, // 100 microseconds (100,000 ns) for HFT
    pub status: String,                 // "IN_BOUNDS" / "DIVERGENCE_ALERT"
}

/// RTS 22 / RTS 25 Report Container
pub struct MifidReport {
    pub firm_lei: String,
    pub report_date: String,
    pub transactions: Vec<MifidTransaction>,
    pub clock_sync_checks: Vec<ClockSyncRecord>,
    pub output_dir: PathBuf,
}

impl MifidReport {
    pub fn new(firm_lei: &str, output_dir: &str) -> Self {
        let now_secs = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();
        let date_str = format!("{}", now_secs / 86400);

        Self {
            firm_lei: firm_lei.to_string(),
            report_date: date_str,
            transactions: Vec::new(),
            clock_sync_checks: Vec::new(),
            output_dir: PathBuf::from(output_dir),
        }
    }

    pub fn add_transaction(&mut self, tx: MifidTransaction) {
        self.transactions.push(tx);
    }

    pub fn add_clock_sync(&mut self, sync: ClockSyncRecord) {
        self.clock_sync_checks.push(sync);
    }

    pub fn write_rts22_xml(&self) -> io::Result<PathBuf> {
        fs::create_dir_all(&self.output_dir)?;
        let path = self.output_dir.join(format!(
            "mifid_rts22_{}_{}.xml",
            self.firm_lei, self.report_date
        ));

        let mut xml = String::with_capacity(1024 + self.transactions.len() * 384);
        xml.push_str("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n");
        xml.push_str("<MifidRTS22Package xmlns=\"urn:esma:mifid:rts22:v1.0\">\n");
        let _ = write!(
            xml,
            "  <Header>\n    <FirmLEI>{}</FirmLEI>\n    <TxCount>{}</TxCount>\n  </Header>\n",
            self.firm_lei,
            self.transactions.len()
        );
        xml.push_str("  <Transactions>\n");
        for tx in &self.transactions {
            xml.push_str(&tx.to_xml());
        }
        xml.push_str("  </Transactions>\n");
        xml.push_str("</MifidRTS22Package>\n");

        let mut file = fs::File::create(&path)?;
        file.write_all(xml.as_bytes())?;
        file.flush()?;

        Ok(path)
    }

    pub fn validate_rts25_compliance(&self) -> (bool, u64) {
        let max_divergence_allowed = 100_000; // 100 microseconds max for HFT
        let mut max_observed = 0;
        let mut all_valid = true;

        for check in &self.clock_sync_checks {
            let abs_offset = check.offset_from_utc_ns.unsigned_abs();
            if abs_offset > max_observed {
                max_observed = abs_offset;
            }
            if abs_offset > max_divergence_allowed {
                all_valid = false;
            }
        }

        (all_valid, max_observed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mifid_rts22_export() {
        let mut report = MifidReport::new("5493006MHB84DD0ZWV18", "/tmp/mifid_test");
        report.add_transaction(MifidTransaction {
            transaction_ref_num: "TX-9901".to_string(),
            trading_venue_mic: "XPAR".to_string(),
            buyer_lei: "5493006MHB84DD0ZWV18".to_string(),
            seller_lei: "213800VBZPP39BL7EU83".to_string(),
            isin: "US0378331005".to_string(),
            instrument_name: "AAPL".to_string(),
            quantity: 1000.0,
            price: 150.25,
            currency: "USD".to_string(),
            trade_timestamp_ns: 1_700_000_000_000_000_000,
            direct_electronic_access: true,
            algo_investment_decision: "MOMENTUM_V2".to_string(),
            algo_execution: "ROBIN_SMART_SOR".to_string(),
            waiver_flag: None,
        });

        let path = report.write_rts22_xml().unwrap();
        assert!(path.exists());
        let _ = fs::remove_file(path);
    }

    #[test]
    fn test_rts25_clock_sync_validation() {
        let mut report = MifidReport::new("5493006MHB84DD0ZWV18", "/tmp/mifid_test");
        report.add_clock_sync(ClockSyncRecord {
            timestamp_ns: 1_700_000_000_000_000_000,
            sync_source: "PTP_IEEE_1588_2008".to_string(),
            offset_from_utc_ns: 420, // 420 nanoseconds offset (well under 100μs)
            max_divergence_allowed_ns: 100_000,
            status: "IN_BOUNDS".to_string(),
        });

        let (compliant, max_div) = report.validate_rts25_compliance();
        assert!(compliant);
        assert_eq!(max_div, 420);
    }
}
