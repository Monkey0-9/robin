#pragma once

#include <cstdint>
#include <atomic>
#include <cstring>
#include <new>

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

/// Lock-free SPSC ring buffer over shared memory (mmap).
/// Used for zero-FFI IPC between hot-path services.
///
/// Layout in shared memory:
///   [0..7]    write_index (atomic, aligned)
///   [64..71]  read_index  (atomic, aligned)
///   [128..]   buffer slots (cache-line aligned each)
///
/// Producer: C++ DPDK network layer
/// Consumer: Rust risk gate
///
/// Total latency: <50ns per message
template<typename T, size_t Capacity>
requires (Capacity > 0 && (Capacity & (Capacity - 1)) == 0)
class ALIGN_PAD_64 LockFreeRingBuffer {
public:
    static constexpr size_t MASK = Capacity - 1;

    LockFreeRingBuffer() noexcept : storage_(nullptr), owned_(false) {}

    void init(void* memory, bool owned = false) noexcept {
        storage_ = static_cast<Storage*>(memory);
        owned_ = owned;
        if (owned_) {
            std::memset(storage_, 0, sizeof(Storage));
        }
    }

    bool push(const T& item) noexcept {
        auto write_idx = storage_->write_idx.load(std::memory_order_relaxed);
        auto read_idx = storage_->read_idx.load(std::memory_order_acquire);

        if ((write_idx - read_idx) >= Capacity) {
            return false;
        }

        storage_->slots[write_idx & MASK] = item;
        storage_->write_idx.store(write_idx + 1, std::memory_order_release);
        return true;
    }

    bool pop(T& item) noexcept {
        auto read_idx = storage_->read_idx.load(std::memory_order_relaxed);
        auto write_idx = storage_->write_idx.load(std::memory_order_acquire);

        if (read_idx == write_idx) {
            return false;
        }

        item = storage_->slots[read_idx & MASK];
        storage_->read_idx.store(read_idx + 1, std::memory_order_release);
        return true;
    }

    size_t size() const noexcept {
        auto w = storage_->write_idx.load(std::memory_order_acquire);
        auto r = storage_->read_idx.load(std::memory_order_acquire);
        return static_cast<size_t>(w - r);
    }

    bool empty() const noexcept {
        return size() == 0;
    }

    static constexpr size_t memory_size() noexcept {
        return sizeof(Storage);
    }

private:
    struct ALIGN_PAD_64 Storage {
        ALIGN_PAD_64 std::atomic<uint64_t> write_idx{0};
        char pad1_[CACHE_LINE_SIZE - sizeof(std::atomic<uint64_t>)];
        ALIGN_PAD_64 std::atomic<uint64_t> read_idx{0};
        char pad2_[CACHE_LINE_SIZE - sizeof(std::atomic<uint64_t>)];
        ALIGN_PAD_64 T slots[Capacity];
    };

    Storage* storage_;
    bool owned_;
};
