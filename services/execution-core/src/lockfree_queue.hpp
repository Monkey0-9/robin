#pragma once

#include <atomic>
#include <cstddef>
#include <cstdint>
#include <cstring>

namespace quantum {
namespace execution {

// Lock-free single-producer single-consumer queue for hot-path order passing.
// Fixed capacity (template param), no heap allocation.
// Uses 64-byte cache-line padding to avoid false sharing.
template <typename T, size_t Capacity>
class LockFreeSPSCQueue {
public:
    static_assert((Capacity & (Capacity - 1)) == 0, "Capacity must be power of 2");
    static constexpr size_t MASK = Capacity - 1;

    LockFreeSPSCQueue() noexcept : head_(0), tail_(0) {}

    bool push(const T& item) noexcept {
        size_t t = tail_.load(std::memory_order_relaxed);
        size_t h = head_.load(std::memory_order_acquire);
        if ((t - h) >= Capacity) return false;
        slots_[t & MASK] = item;
        std::atomic_thread_fence(std::memory_order_release);
        tail_.store(t + 1, std::memory_order_relaxed);
        return true;
    }

    bool pop(T& item) noexcept {
        size_t h = head_.load(std::memory_order_relaxed);
        size_t t = tail_.load(std::memory_order_acquire);
        if (h == t) return false;
        item = slots_[h & MASK];
        std::atomic_thread_fence(std::memory_order_release);
        head_.store(h + 1, std::memory_order_relaxed);
        return true;
    }

    bool empty() const noexcept {
        return head_.load(std::memory_order_relaxed) ==
               tail_.load(std::memory_order_relaxed);
    }

    size_t size() const noexcept {
        return tail_.load(std::memory_order_relaxed) -
               head_.load(std::memory_order_relaxed);
    }

    void clear() noexcept {
        head_.store(0, std::memory_order_relaxed);
        tail_.store(0, std::memory_order_relaxed);
    }

private:
    T slots_[Capacity];
    alignas(64) std::atomic<size_t> head_;
    alignas(64) std::atomic<size_t> tail_;
};

} // namespace execution
} // namespace quantum
