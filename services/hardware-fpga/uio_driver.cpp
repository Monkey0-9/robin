#include <iostream>
#include <fcntl.h>
#include <unistd.h>
#include <sys/mman.h>
#include <cstdint>
#include <stdexcept>

// ============================================================================
// FPGA PCIe UIO Driver Stub
// ============================================================================
// This demonstrates the planned architecture for hardware matching engine
// offload via Userspace I/O (UIO). In a real deployment, the FPGA exposes
// its memory space via /dev/uioX.

class FPGADriver {
private:
    int uio_fd;
    void* mapped_base;
    size_t map_size;

public:
    FPGADriver(const std::string& dev_path, size_t size) : map_size(size) {
        uio_fd = open(dev_path.c_str(), O_RDWR);
        if (uio_fd < 0) {
            // Note: Since this will run in environments without an FPGA, we fallback gracefully
            // so we don't crash the tests/compilation when no device exists.
            std::cerr << "[WARN] Failed to open " << dev_path << ". FPGA UIO disabled.\n";
            mapped_base = nullptr;
            return;
        }

        mapped_base = mmap(nullptr, map_size, PROT_READ | PROT_WRITE, MAP_SHARED, uio_fd, 0);
        if (mapped_base == MAP_FAILED) {
            close(uio_fd);
            uio_fd = -1;
            mapped_base = nullptr;
            throw std::runtime_error("mmap failed for UIO device");
        }
        
        std::cout << "[FPGA] UIO driver successfully mapped " << dev_path << " (size: " << size << ")\n";
    }

    ~FPGADriver() {
        if (mapped_base && mapped_base != MAP_FAILED) {
            munmap(mapped_base, map_size);
        }
        if (uio_fd >= 0) {
            close(uio_fd);
        }
    }

    void write_order(uint64_t order_id, uint32_t inst_id, uint64_t price, uint32_t qty, bool is_buy) {
        if (!mapped_base) {
            std::cout << "[FPGA-FALLBACK] Simulated MMIO write: Order " << order_id << " (Buy=" << is_buy << ")\n";
            return;
        }
        
        // Write to the memory-mapped PCIe registers
        volatile uint64_t* regs = reinterpret_cast<volatile uint64_t*>(mapped_base);
        regs[0] = order_id;
        regs[1] = inst_id;
        regs[2] = price;
        regs[3] = qty;
        regs[4] = is_buy ? 1 : 0;
        
        // Memory barrier
        __sync_synchronize();
    }

    bool check_interrupt() {
        if (uio_fd < 0) return false;
        
        uint32_t info;
        ssize_t ret = read(uio_fd, &info, sizeof(info));
        return (ret == sizeof(info));
    }
};

// Dummy entry point for build verification
int main() {
    try {
        FPGADriver driver("/dev/uio0", 4096);
        driver.write_order(12345, 1, 600000000, 10, true);
    } catch (const std::exception& e) {
        std::cerr << "Driver error: " << e.what() << "\n";
    }
    return 0;
}
