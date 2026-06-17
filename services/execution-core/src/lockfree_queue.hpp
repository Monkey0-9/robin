// services/execution-core/src/lockfree_queue.hpp
// High-performance lock-free SPSC queue using memory barriers and cache line alignment.

#pragma once

#include <atomic>
#include <cstddef>
#include <new>

namespace quantum {
namespace execution {

template <typename T, size_t Capacity>
class LockFreeSPSCQueue {
public:
    static_assert((Capacity & (Capacity - 1)) == 0, "Capacity must be a power of 2");

    LockFreeSPSCQueue() : write_idx_(0), read_idx_(0) {}

    bool push(const T& val) {
        size_t write_idx = write_idx_.load(std::memory_order_relaxed);
        size_t read_idx = read_idx_.load(std::memory_order_acquire);

        if ((write_idx - read_idx) == Capacity) {
            return false; // Queue full
        }

        ring_buffer_[write_idx & (Capacity - 1)] = val;
        write_idx_.store(write_idx + 1, std::memory_order_release);
        return true;
    }

    bool pop(T& val) {
        size_t read_idx = read_idx_.load(std::memory_order_relaxed);
        size_t write_idx = write_idx_.load(std::memory_order_acquire);

        if (read_idx == write_idx) {
            return false; // Queue empty
        }

        val = ring_buffer_[read_idx & (Capacity - 1)];
        read_idx_.store(read_idx + 1, std::memory_order_release);
        return true;
    }

    bool empty() const {
        return read_idx_.load(std::memory_order_relaxed) == write_idx_.load(std::memory_order_relaxed);
    }

    size_t size() const {
        size_t write = write_idx_.load(std::memory_order_relaxed);
        size_t read = read_idx_.load(std::memory_order_relaxed);
        return (write >= read) ? (write - read) : (Capacity - (read - write));
    }

private:
    alignas(64) T ring_buffer_[Capacity];
    alignas(64) std::atomic<size_t> write_idx_;
    alignas(64) std::atomic<size_t> read_idx_;
};

} // namespace execution
} // namespace quantum
