use std::sync::atomic::{AtomicU64, Ordering};
use std::fs::OpenOptions;
use memmap2::MmapOptions;

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
    pub mmap: memmap2::MmapMut,
    pub header: *mut ShmHeader,
    pub ring: *mut ShmMessage,
}

unsafe impl Send for ShmBridge {}
unsafe impl Sync for ShmBridge {}

impl ShmBridge {
    pub fn new(path: &str, create: bool) -> Result<Self, String> {
        let shm_size = std::mem::size_of::<ShmHeader>() + SHM_CAPACITY * SHM_MSG_SIZE;
        
        let file = if create {
            OpenOptions::new()
                .read(true)
                .write(true)
                .create(true)
                .open(path)
                .map_err(|e| format!("Failed to create shm file: {}", e))?
        } else {
            OpenOptions::new()
                .read(true)
                .write(true)
                .open(path)
                .map_err(|e| format!("Failed to open shm file: {}", e))?
        };

        if create {
            file.set_len(shm_size as u64)
                .map_err(|e| format!("Failed to truncate shm file: {}", e))?;
        }

        let mut mmap = unsafe {
            MmapOptions::new()
                .len(shm_size)
                .map_mut(&file)
                .map_err(|e| format!("Failed to mmap file: {}", e))?
        };

        let header = mmap.as_mut_ptr() as *mut ShmHeader;
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

        let ring = unsafe { mmap.as_mut_ptr().add(std::mem::size_of::<ShmHeader>()) as *mut ShmMessage };

        Ok(Self {
            mmap,
            header,
            ring,
        })
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

    pub fn unlink(path: &str) {
        let _ = std::fs::remove_file(path);
    }
}

#[cfg(test)]
mod tests {
    use super::{ShmBridge, ShmMessage};

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
