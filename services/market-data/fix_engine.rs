// FIX Engine (Financial Information eXchange)
// Built for MiFID II compliant ultra-low latency connectivity
use std::net::TcpStream;
use std::io::{Write, Read};

pub struct FixEngine {
    pub target_comp_id: String,
    pub sender_comp_id: String,
    pub msg_seq_num: u32,
    stream: Option<TcpStream>,
}

impl FixEngine {
    pub fn new(target: &str, sender: &str) -> Self {
        Self {
            target_comp_id: target.to_string(),
            sender_comp_id: sender.to_string(),
            msg_seq_num: 1,
            stream: None,
        }
    }

    pub fn connect(&mut self, address: &str) -> Result<(), std::io::Error> {
        let stream = TcpStream::connect(address)?;
        self.stream = Some(stream);
        self.send_logon()
    }

    fn send_logon(&mut self) -> Result<(), std::io::Error> {
        let logon_msg = self.build_message("A", "98=0\x01108=30\x01");
        self.send_raw(&logon_msg)
    }

    pub fn send_new_order_single(&mut self, cl_ord_id: &str, symbol: &str, side: char, qty: u64, price: f64) -> Result<(), std::io::Error> {
        // MsgType = D (New Order Single)
        let body = format!("11={}\x0155={}\x0154={}\x0138={}\x0140=2\x0144={}\x01", cl_ord_id, symbol, side, qty, price);
        let msg = self.build_message("D", &body);
        self.send_raw(&msg)
    }

    fn build_message(&mut self, msg_type: &str, body: &str) -> String {
        let header = format!("35={}\x0149={}\x0156={}\x0134={}\x01", msg_type, self.sender_comp_id, self.target_comp_id, self.msg_seq_num);
        let raw_msg = format!("{}{}", header, body);
        let len = raw_msg.len();
        let fix_msg = format!("8=FIX.4.4\x019={}\x01{}", len, raw_msg);
        
        let mut checksum = 0;
        for b in fix_msg.bytes() {
            checksum = (checksum + b as u32) % 256;
        }
        self.msg_seq_num += 1;
        format!("{}10={:03}\x01", fix_msg, checksum)
    }

    fn send_raw(&mut self, msg: &str) -> Result<(), std::io::Error> {
        if let Some(stream) = self.stream.as_mut() {
            stream.write_all(msg.as_bytes())?;
            println!("[FIX OUT] {}", msg.replace("\x01", "|"));
        }
        Ok(())
    }
}
