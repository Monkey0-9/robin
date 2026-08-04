use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::thread;

pub struct HardwareKillSwitch {
    active: Arc<AtomicBool>,
    trigger_count: Arc<AtomicU64>,
    last_trigger_ns: Arc<AtomicU64>,
    monitor_handle: Option<thread::JoinHandle<()>>,
}

impl Default for HardwareKillSwitch {
    fn default() -> Self {
        Self::new()
    }
}

impl HardwareKillSwitch {
    pub fn new() -> Self {
        Self {
            active: Arc::new(AtomicBool::new(false)),
            trigger_count: Arc::new(AtomicU64::new(0)),
            last_trigger_ns: Arc::new(AtomicU64::new(0)),
            monitor_handle: None,
        }
    }

    pub fn start_monitoring(&mut self) {
        let active = self.active.clone();
        let trigger_count = self.trigger_count.clone();
        let last_trigger_ns = self.last_trigger_ns.clone();
        let handle = thread::spawn(move || {
            #[cfg(target_os = "linux")]
            {
                use std::fs::OpenOptions;
                use std::io::{Read, Seek, SeekFrom};
                use std::os::fd::AsRawFd;

                let file = OpenOptions::new()
                    .read(true)
                    .open("/sys/class/gpio/gpio18/value");
                if let Ok(mut f) = file {
                    let fd = f.as_raw_fd();
                    let mut pfd = libc::pollfd {
                        fd,
                        events: libc::POLLPRI | libc::POLLERR,
                        revents: 0,
                    };

                    let mut buf = [0u8; 2];
                    loop {
                        let _ = f.seek(SeekFrom::Start(0));
                        let _ = f.read(&mut buf);

                        let ret = unsafe { libc::poll(&mut pfd as *mut _, 1, -1) };
                        if ret > 0 && (pfd.revents & libc::POLLPRI) != 0 {
                            let _ = f.seek(SeekFrom::Start(0));
                            if let Ok(n) = f.read(&mut buf) {
                                if n > 0 && buf[0] == b'1' {
                                    println!("[KILL_SWITCH] GPIO trigger detected");
                                    active.store(true, Ordering::Release);
                                    trigger_count.fetch_add(1, Ordering::Relaxed);
                                    last_trigger_ns.store(
                                        std::time::SystemTime::now()
                                            .duration_since(std::time::UNIX_EPOCH)
                                            .unwrap_or_default()
                                            .as_nanos()
                                            as u64,
                                        Ordering::Relaxed,
                                    );
                                    println!("[KILL_SWITCH] ACTIVATED");
                                    break;
                                }
                            }
                        }
                    }
                }
            }

            #[cfg(not(target_os = "linux"))]
            {
                loop {
                    thread::sleep(Duration::from_secs(1));
                }
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
