use std::sync::atomic::{AtomicU64, Ordering};

#[repr(C, align(64))]
pub struct ShmHeader {
    pub write_idx: AtomicU64,
    pub read_idx: AtomicU64,
    pub magic: u64,
    pub version: u32,
    pub size: u32,
    pub pid_writer: u32,
    pub pid_reader: u32,
    _pad: [u8; 24],
}

pub const SHM_MAGIC: u64 = 0x524f42494e484d5f;
pub const SHM_VERSION: u32 = 1;
pub const SHM_CAPACITY: usize = 65536;
pub const SHM_MSG_SIZE: usize = 64;

#[derive(Clone, Copy)]
#[repr(C, align(64))]
pub struct ShmMessage {
    pub msg_type: u8,
    pub client_id: u32,
    pub instrument_id: u32,
    pub price: u32,
    pub qty: u32,
    pub side: u8,
    pub flags: u8,
    pub order_id: u64,
    pub cl_order_id: u64,
    pub timestamp_ns: u64,
    pub _pad: [u8; 21],
}

pub struct ShmBridge {
    pub fd: i32,
    pub mapped_addr: *mut u8,
    pub size: usize,
    pub header: *mut ShmHeader,
    pub ring: *mut ShmMessage,
}

unsafe impl Send for ShmBridge {}
unsafe impl Sync for ShmBridge {}

impl ShmBridge {
    pub fn new(path: &str, create: bool) -> Result<Self, String> {
        let shm_size = std::mem::size_of::<ShmHeader>() + SHM_CAPACITY * SHM_MSG_SIZE;
        #[cfg(not(target_os = "linux"))]
        let _ = shm_size; // Used only on Linux for ftruncate

        #[cfg(target_os = "linux")]
        {
            let cpath = std::ffi::CString::new(path).map_err(|e| e.to_string())?;
            let fd = unsafe {
                libc::shm_open(
                    cpath.as_ptr(),
                    if create {
                        libc::O_CREAT | libc::O_RDWR
                    } else {
                        libc::O_RDWR
                    },
                    0o600,
                )
            };
            if fd < 0 {
                return Err(format!("shm_open failed: {}", std::io::Error::last_os_error()));
            }

            if create {
                let ret = unsafe { libc::ftruncate(fd, shm_size as i64) };
                if ret < 0 {
                    unsafe { libc::close(fd) };
                    return Err(format!("ftruncate failed: {}", std::io::Error::last_os_error()));
                }
            }

            let mapped_addr = unsafe {
                libc::mmap(
                    std::ptr::null_mut(),
                    shm_size,
                    libc::PROT_READ | libc::PROT_WRITE,
                    libc::MAP_SHARED,
                    fd,
                    0,
                )
            } as *mut u8;
            if mapped_addr == libc::MAP_FAILED as *mut u8 {
                unsafe { libc::close(fd) };
                return Err(format!("mmap failed: {}", std::io::Error::last_os_error()));
            }

            unsafe { libc::close(fd) };

            let header = mapped_addr as *mut ShmHeader;
            if create {
                unsafe {
                    (*header).write_idx = AtomicU64::new(0);
                    (*header).read_idx = AtomicU64::new(0);
                    (*header).magic = SHM_MAGIC;
                    (*header).version = SHM_VERSION;
                    (*header).size = SHM_CAPACITY as u32;
                    (*header).pid_writer = std::process::id();
                    (*header).pid_reader = 0;
                }
            }

            let ring = unsafe { mapped_addr.add(std::mem::size_of::<ShmHeader>()) as *mut ShmMessage };

            Ok(Self {
                fd: -1,
                mapped_addr,
                size: shm_size,
                header,
                ring,
            })
        }

        #[cfg(not(target_os = "linux"))]
        {
            let _ = path;
            let _ = create;
            Err("Shared memory requires Linux".to_string())
        }
    }

    #[inline(always)]
    pub fn push(&mut self, msg: &ShmMessage) -> bool {
        let header = unsafe { &*self.header };
        let write_idx = header.write_idx.load(Ordering::Relaxed);
        let read_idx = header.read_idx.load(Ordering::Acquire);

        if write_idx - read_idx >= SHM_CAPACITY as u64 {
            return false;
        }

        let slot = (write_idx & (SHM_CAPACITY as u64 - 1)) as usize;
        unsafe {
            std::ptr::copy_nonoverlapping(msg as *const ShmMessage, self.ring.add(slot), 1);
            std::sync::atomic::fence(Ordering::Release);
            header.write_idx.store(write_idx + 1, Ordering::Relaxed);
        }
        true
    }

    #[inline(always)]
    pub fn pop(&mut self, msg: &mut ShmMessage) -> bool {
        let header = unsafe { &*self.header };
        let read_idx = header.read_idx.load(Ordering::Relaxed);
        let write_idx = header.write_idx.load(Ordering::Acquire);

        if read_idx == write_idx {
            return false;
        }

        let slot = (read_idx & (SHM_CAPACITY as u64 - 1)) as usize;
        unsafe {
            std::ptr::copy_nonoverlapping(self.ring.add(slot), msg as *mut ShmMessage, 1);
            std::sync::atomic::fence(Ordering::Release);
            header.read_idx.store(read_idx + 1, Ordering::Relaxed);
        }
        true
    }

    #[inline(always)]
    pub fn forward_order(&mut self, order: &crate::gate::Order) -> bool {
        let msg = ShmMessage {
            msg_type: 1,
            client_id: order.client_id,
            instrument_id: order.instrument_id,
            price: order.price,
            qty: order.qty,
            side: order.side as u8,
            flags: 0,
            order_id: order.id,
            cl_order_id: order.cl_order_id,
            timestamp_ns: order.timestamp,
            _pad: [0u8; 21],
        };
        self.push(&msg)
    }

    #[inline(always)]
    pub fn available(&self) -> u64 {
        let header = unsafe { &*self.header };
        header.write_idx.load(Ordering::Relaxed) - header.read_idx.load(Ordering::Relaxed)
    }

    pub fn unlink(_path: &str) {
        #[cfg(target_os = "linux")]
        {
            let cpath = std::ffi::CString::new(_path).unwrap();
            unsafe { libc::shm_unlink(cpath.as_ptr()) };
        }
    }
}

impl Drop for ShmBridge {
    fn drop(&mut self) {
        #[cfg(target_os = "linux")]
        {
            if !self.mapped_addr.is_null() {
                unsafe { libc::munmap(self.mapped_addr as *mut libc::c_void, self.size) };
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use std::mem::MaybeUninit;
    use super::*;

    #[test]
    #[cfg(target_os = "linux")]
    fn test_shm_bridge() {
        let path = "/robin_risk_test";
        let mut bridge = ShmBridge::new(path, true).unwrap();

        let msg = ShmMessage {
            msg_type: 1,
            client_id: 42,
            instrument_id: 100,
            price: 50000,
            qty: 100,
            side: 0,
            flags: 0,
            order_id: 1,
            cl_order_id: 1001,
            timestamp_ns: 1234567890,
            _pad: [0u8; 21],
        };

        assert!(bridge.push(&msg));
        assert_eq!(bridge.available(), 1);

        let mut received = unsafe {
            let mut m = MaybeUninit::<ShmMessage>::zeroed();
            m.assume_init()
        };
        assert!(bridge.pop(&mut received));
        assert_eq!(received.order_id, 1);
        assert_eq!(received.price, 50000);

        ShmBridge::unlink(path);
    }
}
