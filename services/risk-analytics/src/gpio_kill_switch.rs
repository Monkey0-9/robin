use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::thread;
use std::time::Duration;

pub struct HardwareKillSwitch {
    active: AtomicBool,
    trigger_count: AtomicU64,
    last_trigger_ns: AtomicU64,
    monitor_handle: Option<thread::JoinHandle<()>>,
}

impl HardwareKillSwitch {
    pub fn new() -> Self {
        Self {
            active: AtomicBool::new(false),
            trigger_count: AtomicU64::new(0),
            last_trigger_ns: AtomicU64::new(0),
            monitor_handle: None,
        }
    }

    pub fn start_monitoring(&mut self) {
        let handle = thread::spawn(move || {
            #[cfg(target_os = "linux")]
            {
                const GPIO_PATH: &str = "/sys/class/gpio/gpio18/value";
                loop {
                    if let Ok(content) = std::fs::read_to_string(GPIO_PATH) {
                        if content.trim() == "1" {
                            println!("[KILL_SWITCH] GPIO trigger detected");
                            break;
                        }
                    }
                    thread::sleep(Duration::from_millis(1));
                }
            }

            #[cfg(not(target_os = "linux"))]
            {
                thread::sleep(Duration::from_secs(3600));
            }
        });
        self.monitor_handle = Some(handle);
    }

    #[inline(always)]
    pub fn is_active(&self) -> bool {
        self.active.load(Ordering::Acquire)
    }

    pub fn trigger(&mut self) {
        self.active.store(true, Ordering::Release);
        self.trigger_count.fetch_add(1, Ordering::Relaxed);
        self.last_trigger_ns.store(
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos() as u64,
            Ordering::Relaxed,
        );
        println!("[KILL_SWITCH] ACTIVATED");
    }

    pub fn clear(&mut self) {
        self.active.store(false, Ordering::Release);
        println!("[KILL_SWITCH] CLEARED");
    }

    pub fn get_trigger_count(&self) -> u64 {
        self.trigger_count.load(Ordering::Relaxed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_kill_switch() {
        let mut ks = HardwareKillSwitch::new();
        assert!(!ks.is_active());
        ks.trigger();
        assert!(ks.is_active());
        assert_eq!(ks.get_trigger_count(), 1);
    }
}
