import mmap
import struct
import time
import os

class ShmBridge:
    def __init__(self, filename="robin_ai_oms.shm", capacity=1024):
        self.filename = filename
        self.capacity = capacity
        # 192 bytes for header, 64 bytes per message
        self.shm_size = 192 + (self.capacity * 64)
        
        # Open or create the file
        if not os.path.exists(self.filename):
            with open(self.filename, "wb") as f:
                f.write(b'\x00' * self.shm_size)
                
        self.f = open(self.filename, "r+b")
        self.mm = mmap.mmap(self.f.fileno(), self.shm_size, access=mmap.ACCESS_WRITE)
        
        # Initialize header if it's all zeros
        # write_idx (8), pad1(56), read_idx (8), pad2(56), magic (8), version (4), size (4)
        magic = struct.unpack_from("<Q", self.mm, 128)[0]
        if magic == 0:
            # write magic = 0x524f42494e484d5f, version = 1, size = capacity
            struct.pack_into("<Q", self.mm, 128, 0x524f42494e484d5f)
            struct.pack_into("<I", self.mm, 136, 1)
            struct.pack_into("<I", self.mm, 140, self.capacity)

    def send_order(self, instrument_id: int, price: int, qty: int, side: int):
        # Read indices
        write_idx = struct.unpack_from("<Q", self.mm, 0)[0]
        read_idx = struct.unpack_from("<Q", self.mm, 64)[0]
        
        if write_idx - read_idx >= self.capacity:
            return False # Queue full
            
        slot = write_idx % self.capacity
        offset = 192 + (slot * 64)
        
        msg_type = 1
        client_id = 99 # AI Agent client ID
        flags = 0
        order_id = int(time.time() * 1e9)
        cl_order_id = order_id
        timestamp_ns = order_id
        
        # Pack message (64 bytes):
        # msg_type(1), pad(3), client_id(4), instrument_id(4), pad(4), price(8), qty(8), side(1), flags(1), pad(6), order_id(8), cl_order_id(8), timestamp(8), pad(8)
        # B 3x B I I 4x Q Q B B 6x Q Q Q 8x
        # Wait, the Rust struct:
        # msg_type (u8), client_id (u32, offset 4), instrument_id (u32, offset 8), price (u64, offset 16), qty (u64, offset 24), side (u8, offset 32), flags (u8, offset 33), order_id (u64, offset 40), cl_order_id (u64, offset 48), timestamp_ns (u64, offset 56)
        
        struct.pack_into("<B 3x I I 4x Q Q B B 6x Q Q Q", self.mm, offset,
                         msg_type, client_id, instrument_id, price, qty, side, flags, order_id, cl_order_id, timestamp_ns)
        
        # Increment write_idx (Release semantics - in python we just write)
        struct.pack_into("<Q", self.mm, 0, write_idx + 1)
        
        return True
