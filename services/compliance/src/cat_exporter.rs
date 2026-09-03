// ============================================================================
// CAT (Consolidated Audit Trail) XML Exporter
// (services/compliance/src/cat_exporter.rs)
// ============================================================================
// Generates daily CAT report files in XML format conforming to FINRA CAT
// Specification Version 2.2 (SEC Rule 613).
//
// Each report covers order lifecycle events:
//   - NEW ORDER (event type: NEW)
//   - ORDER CANCEL (event type: CXLO)
//   - ORDER FILL (event type: EXEC)
//   - ORDER REJECT (event type: REJI)
//
// Output: `cat_reports/YYYYMMDD_<firm_id>_cat.xml`
//
// Reference: https://www.catnmsplan.com/technical-specifications
// ============================================================================

use std::fmt::Write as FmtWrite;
use std::fs;
use std::io::{self, Write};
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

/// CAT order event type (SEC Rule 613 event codes).
#[derive(Debug, Clone, PartialEq)]
pub enum CatEventType {
    /// New order received
    NewOrder,
    /// Order cancelled
    Cancel,
    /// Order fully/partially executed
    Execution,
    /// Order rejected
    Reject,
    /// Order modified (cancel-replace)
    Modification,
    /// Trade report
    TradeReport,
}

impl CatEventType {
    fn code(&self) -> &'static str {
        match self {
            CatEventType::NewOrder => "NEW",
            CatEventType::Cancel => "CXLO",
            CatEventType::Execution => "EXEC",
            CatEventType::Reject => "REJI",
            CatEventType::Modification => "MDFY",
            CatEventType::TradeReport => "TRPR",
        }
    }
}

/// Side of an order.
#[derive(Debug, Clone, PartialEq)]
pub enum CatSide {
    Buy,
    Sell,
    SellShort,
    SellShortExempt,
}

impl CatSide {
    fn code(&self) -> &'static str {
        match self {
            CatSide::Buy => "B",
            CatSide::Sell => "S",
            CatSide::SellShort => "SS",
            CatSide::SellShortExempt => "SSE",
        }
    }
}

/// A single CAT event record.
#[derive(Debug, Clone)]
pub struct CatEvent {
    /// Unique order identifier (OMS-assigned)
    pub order_id: String,
    /// Client order ID (as provided by customer)
    pub cl_order_id: String,
    /// Account/customer identifier
    pub account_id: String,
    /// Registered firm MPID (Market Participant Identifier)
    pub firm_mpid: String,
    /// Symbol (e.g. "AAPL")
    pub symbol: String,
    /// Market center (exchange MIC, e.g. "XNAS")
    pub market_center: String,
    /// Event type
    pub event_type: CatEventType,
    /// Order side
    pub side: CatSide,
    /// Order quantity (shares)
    pub qty: u64,
    /// Limit price in dollars (None for market orders)
    pub price: Option<f64>,
    /// Executed price (for fills)
    pub exec_price: Option<f64>,
    /// Executed quantity (for fills)
    pub exec_qty: Option<u64>,
    /// Event timestamp (nanoseconds since Unix epoch)
    pub timestamp_ns: u64,
    /// Whether the order was submitted via an algorithm
    pub algo_indicator: bool,
    /// Reason code (for rejects)
    pub reject_reason: Option<String>,
}

impl CatEvent {
    /// Format timestamp as ISO 8601 with nanosecond precision.
    fn timestamp_iso(&self) -> String {
        let secs = self.timestamp_ns / 1_000_000_000;
        let ns = self.timestamp_ns % 1_000_000_000;
        // Convert unix secs to UTC datetime string manually
        let dt = format_unix_secs(secs);
        format!("{}.{:09}Z", dt, ns)
    }

    /// Format price as fixed-point string (6 decimal places, CAT spec §3.1.4)
    fn fmt_price(p: f64) -> String {
        format!("{:.6}", p)
    }

    /// Render this event as an XML `<OrderEvent>` element.
    pub fn to_xml(&self) -> String {
        let mut s = String::new();
        let _ = writeln!(s, "  <OrderEvent>");
        let _ = writeln!(
            s,
            "    <EventType>{}</EventType>",
            escape_xml(self.event_type.code())
        );
        let _ = writeln!(s, "    <OrderID>{}</OrderID>", escape_xml(&self.order_id));
        let _ = writeln!(
            s,
            "    <ClOrderID>{}</ClOrderID>",
            escape_xml(&self.cl_order_id)
        );
        let _ = writeln!(
            s,
            "    <AccountID>{}</AccountID>",
            escape_xml(&self.account_id)
        );
        let _ = writeln!(
            s,
            "    <FirmMPID>{}</FirmMPID>",
            escape_xml(&self.firm_mpid)
        );
        let _ = writeln!(s, "    <Symbol>{}</Symbol>", escape_xml(&self.symbol));
        let _ = writeln!(
            s,
            "    <MarketCenter>{}</MarketCenter>",
            escape_xml(&self.market_center)
        );
        let _ = writeln!(s, "    <Side>{}</Side>", escape_xml(self.side.code()));
        let _ = writeln!(s, "    <Quantity>{}</Quantity>", self.qty);
        if let Some(p) = self.price {
            let _ = writeln!(s, "    <LimitPrice>{}</LimitPrice>", Self::fmt_price(p));
        }
        if let Some(ep) = self.exec_price {
            let _ = writeln!(
                s,
                "    <ExecutionPrice>{}</ExecutionPrice>",
                Self::fmt_price(ep)
            );
        }
        if let Some(eq) = self.exec_qty {
            let _ = writeln!(s, "    <ExecutionQty>{}</ExecutionQty>", eq);
        }
        let _ = writeln!(
            s,
            "    <EventTimestamp>{}</EventTimestamp>",
            self.timestamp_iso()
        );
        let _ = writeln!(
            s,
            "    <AlgoIndicator>{}</AlgoIndicator>",
            if self.algo_indicator { "Y" } else { "N" }
        );
        if let Some(ref reason) = self.reject_reason {
            let _ = writeln!(s, "    <RejectReason>{}</RejectReason>", escape_xml(reason));
        }
        let _ = writeln!(s, "  </OrderEvent>");
        s
    }
}

/// CAT daily report file.
pub struct CatReport {
    pub firm_mpid: String,
    pub report_date: String, // YYYYMMDD
    pub events: Vec<CatEvent>,
    pub output_dir: PathBuf,
}

impl CatReport {
    pub fn new(firm_mpid: &str, output_dir: &str) -> Self {
        let date = today_yyyymmdd();
        Self {
            firm_mpid: firm_mpid.to_string(),
            report_date: date,
            events: Vec::new(),
            output_dir: PathBuf::from(output_dir),
        }
    }

    pub fn add_event(&mut self, event: CatEvent) {
        self.events.push(event);
    }

    pub fn event_count(&self) -> usize {
        self.events.len()
    }

    /// Write the report to disk as XML.
    /// File: `<output_dir>/<YYYYMMDD>_<firm_mpid>_cat.xml`
    pub fn write(&self) -> io::Result<PathBuf> {
        fs::create_dir_all(&self.output_dir)?;
        let filename = format!(
            "{}_{}_{}_cat.xml",
            self.report_date,
            self.firm_mpid,
            self.events.len()
        );
        let path = self.output_dir.join(&filename);

        let mut xml = String::with_capacity(1024 + self.events.len() * 512);
        xml.push_str("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n");
        xml.push_str("<CATReport xmlns=\"urn:cat:report:v2.2\"\n");
        xml.push_str("           xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n");
        let _ = writeln!(xml, "  <ReportHeader>");
        let _ = writeln!(
            xml,
            "    <FirmMPID>{}</FirmMPID>",
            escape_xml(&self.firm_mpid)
        );
        let _ = writeln!(xml, "    <ReportDate>{}</ReportDate>", self.report_date);
        let _ = writeln!(xml, "    <ReportTimestamp>{}</ReportTimestamp>", now_iso());
        let _ = writeln!(xml, "    <EventCount>{}</EventCount>", self.events.len());
        let _ = writeln!(xml, "    <SpecificationVersion>2.2</SpecificationVersion>");
        let _ = writeln!(xml, "  </ReportHeader>");
        let _ = writeln!(xml, "  <OrderEvents>");
        for event in &self.events {
            xml.push_str(&event.to_xml());
        }
        let _ = writeln!(xml, "  </OrderEvents>");
        xml.push_str("</CATReport>\n");

        let mut file = fs::File::create(&path)?;
        file.write_all(xml.as_bytes())?;
        file.flush()?;

        eprintln!(
            "[cat] Report written: {} ({} events)",
            path.display(),
            self.events.len()
        );
        Ok(path)
    }

    /// Validate the report: check for required fields on all events.
    /// Returns a list of validation errors.
    pub fn validate(&self) -> Vec<String> {
        let mut errors = Vec::new();
        for (i, ev) in self.events.iter().enumerate() {
            if ev.order_id.is_empty() {
                errors.push(format!("Event[{}]: order_id is empty", i));
            }
            if ev.symbol.is_empty() {
                errors.push(format!("Event[{}]: symbol is empty", i));
            }
            if ev.firm_mpid.is_empty() {
                errors.push(format!("Event[{}]: firm_mpid is empty", i));
            }
            if ev.account_id.is_empty() {
                errors.push(format!("Event[{}]: account_id is empty", i));
            }
            if ev.timestamp_ns == 0 {
                errors.push(format!("Event[{}]: timestamp_ns is 0", i));
            }
            if ev.event_type == CatEventType::Execution {
                if ev.exec_price.is_none() {
                    errors.push(format!("Event[{}]: EXEC missing exec_price", i));
                }
                if ev.exec_qty.is_none() {
                    errors.push(format!("Event[{}]: EXEC missing exec_qty", i));
                }
            }
        }
        errors
    }
}

// ============================================================================
// Helpers
// ============================================================================

fn escape_xml(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}

fn now_ns() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos() as u64
}

fn now_iso() -> String {
    let ns = now_ns();
    let secs = ns / 1_000_000_000;
    let sub_ns = ns % 1_000_000_000;
    format!("{}.{:09}Z", format_unix_secs(secs), sub_ns)
}

fn today_yyyymmdd() -> String {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    let days = secs / 86400;
    // Use a simple Julian day → Gregorian conversion
    let (y, m, d) = jdn_to_ymd(days as i64 + 2440588); // Unix epoch = JDN 2440588
    format!("{:04}{:02}{:02}", y, m, d)
}

/// Format Unix seconds as "YYYY-MM-DDTHH:MM:SS" (UTC, no sub-second).
fn format_unix_secs(secs: u64) -> String {
    let s = secs % 60;
    let min = (secs / 60) % 60;
    let h = (secs / 3600) % 24;
    let days = secs / 86400;
    let (y, m, d) = jdn_to_ymd(days as i64 + 2440588);
    format!("{:04}-{:02}-{:02}T{:02}:{:02}:{:02}", y, m, d, h, min, s)
}

/// Julian Day Number → (year, month, day) — Gregorian calendar.
fn jdn_to_ymd(jdn: i64) -> (i64, i64, i64) {
    let l = jdn + 68569;
    let n = (4 * l) / 146097;
    let l = l - (146097 * n + 3) / 4;
    let i = (4000 * (l + 1)) / 1461001;
    let l = l - (1461 * i) / 4 + 31;
    let j = (80 * l) / 2447;
    let d = l - (2447 * j) / 80;
    let l = j / 11;
    let m = j + 2 - (12 * l);
    let y = 100 * (n - 49) + i + l;
    (y, m, d)
}

// ============================================================================
// Tests
// ============================================================================
#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    fn sample_event(event_type: CatEventType) -> CatEvent {
        CatEvent {
            order_id: "ORD-001".to_string(),
            cl_order_id: "CL-001".to_string(),
            account_id: "ACC-001".to_string(),
            firm_mpid: "ROBN".to_string(),
            symbol: "AAPL".to_string(),
            market_center: "XNAS".to_string(),
            event_type,
            side: CatSide::Buy,
            qty: 100,
            price: Some(150.25),
            exec_price: None,
            exec_qty: None,
            timestamp_ns: 1_700_000_000_000_000_000,
            algo_indicator: true,
            reject_reason: None,
        }
    }

    #[test]
    fn test_cat_event_to_xml() {
        let ev = sample_event(CatEventType::NewOrder);
        let xml = ev.to_xml();
        assert!(xml.contains("<EventType>NEW</EventType>"));
        assert!(xml.contains("<Symbol>AAPL</Symbol>"));
        assert!(xml.contains("<AlgoIndicator>Y</AlgoIndicator>"));
        assert!(xml.contains("<Side>B</Side>"));
    }

    #[test]
    fn test_cat_report_write() {
        let mut report = CatReport::new("ROBN", "/tmp/cat_test");
        report.add_event(sample_event(CatEventType::NewOrder));
        report.add_event({
            let mut ev = sample_event(CatEventType::Execution);
            ev.exec_price = Some(150.30);
            ev.exec_qty = Some(100);
            ev
        });

        let path = report.write().expect("Failed to write CAT report");
        assert!(path.exists());

        let contents = fs::read_to_string(&path).unwrap();
        assert!(contents.contains("<CATReport"));
        assert!(contents.contains("<EventType>NEW</EventType>"));
        assert!(contents.contains("<EventType>EXEC</EventType>"));
        assert!(contents.contains("<EventCount>2</EventCount>"));

        // Cleanup
        fs::remove_file(path).ok();
    }

    #[test]
    fn test_cat_report_validation() {
        let mut report = CatReport::new("ROBN", "/tmp/cat_test");
        // Add a malformed event
        report.add_event(CatEvent {
            order_id: "".to_string(), // EMPTY — should fail
            cl_order_id: "".to_string(),
            account_id: "ACC".to_string(),
            firm_mpid: "ROBN".to_string(),
            symbol: "".to_string(), // EMPTY — should fail
            market_center: "XNAS".to_string(),
            event_type: CatEventType::Execution,
            side: CatSide::Buy,
            qty: 100,
            price: Some(100.0),
            exec_price: None, // MISSING — should fail for EXEC
            exec_qty: None,   // MISSING — should fail for EXEC
            timestamp_ns: 0,  // ZERO — should fail
            algo_indicator: false,
            reject_reason: None,
        });

        let errors = report.validate();
        assert!(!errors.is_empty(), "Should have validation errors");
        assert!(errors.iter().any(|e| e.contains("order_id")));
        assert!(errors.iter().any(|e| e.contains("symbol")));
        assert!(errors.iter().any(|e| e.contains("exec_price")));
    }

    #[test]
    fn test_xml_escaping() {
        let s = r#"A&B<C>D"E'F"#;
        let escaped = escape_xml(s);
        assert_eq!(escaped, "A&amp;B&lt;C&gt;D&quot;E&apos;F");
        assert!(!escaped.contains('<'));
        assert!(!escaped.contains('>'));
    }

    #[test]
    fn test_timestamp_format() {
        let ev = sample_event(CatEventType::NewOrder);
        let ts = ev.timestamp_iso();
        // Should end in Z (UTC)
        assert!(ts.ends_with('Z'), "Timestamp must be UTC: {}", ts);
        // Should contain T separator
        assert!(ts.contains('T'), "Timestamp must contain T: {}", ts);
    }
}
