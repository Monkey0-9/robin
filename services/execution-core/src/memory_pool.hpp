#pragma once
// ============================================================================
// NUMA-Aware Memory Pool Allocator (services/execution-core/src/memory_pool.hpp)
// ============================================================================
// Lock-free ABA-safe pool allocator backed by:
//   - Linux: MAP_HUGETLB + MAP_LOCKED (2MB huge pages pinned in RAM)
//   - Windows: _aligned_malloc + VirtualLock
//   - Other:  posix_memalign
//
// Design principles:
//   • Single pre-allocated slab — no fragmentation, deterministic latency
//   • 64-byte aligned slots — one slot per cache line
//   • Lock-free allocate/deallocate via CAS with ABA counter
//   • NUMA binding to node 0 (co-located with NIC) on Linux
//   • prefault_pool() helper: pre-touches pages at startup
// ============================================================================

#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <cassert>
#include <atomic>
#include <new>
#include <stdexcept>
#include <vector>

#if defined(__linux__)
#  include <sys/mman.h>
#  include <unistd.h>
#  ifdef __has_include
#    if __has_include(<numa.h>)
#      include <numa.h>
#      include <numaif.h>
#      define ROBIN_HAS_NUMA 1
#    endif
#  endif
#endif

#if defined(_WIN32)
#  include <windows.h>
#endif

namespace quantum {
namespace execution {

// ============================================================================
// SlabAllocator<T, Capacity>
// ============================================================================
// Lock-free LIFO free-list over a contiguous, pre-faulted slab.
// Capacity must be a power of two (compile-time assertion).
// ============================================================================
template<typename T, size_t Capacity = (1 << 20)>
class alignas(64) SlabAllocator {
    static_assert((Capacity & (Capacity - 1)) == 0,
                  "Capacity must be a power of 2");

    // Upper 16 bits: ABA counter; lower 48 bits: slot index
    static constexpr uint64_t MASK  = (uint64_t(1) << 48) - 1;
    static constexpr uint64_t EMPTY = MASK;

    struct alignas(64) FreeNode {
        std::atomic<uint64_t> next{EMPTY};
        uint8_t _pad[56]; // pad to 64 bytes
    };

public:
    SlabAllocator() {
        alloc_slab();
        build_freelist();
    }

    ~SlabAllocator() {
        free_slab();
        delete[] freelist_;
    }

    SlabAllocator(const SlabAllocator&) = delete;
    SlabAllocator& operator=(const SlabAllocator&) = delete;

    /// Acquire one slot. Returns nullptr on pool exhaustion.
    [[nodiscard]] T* acquire() noexcept {
        uint64_t head = free_head_.load(std::memory_order_acquire);
        for (;;) {
            const uint64_t idx = head & MASK;
            if (idx == EMPTY) return nullptr;

            const uint64_t nxt = freelist_[idx].next.load(std::memory_order_relaxed);
            // Bump ABA tag in upper 16 bits to prevent ABA problem
            const uint64_t new_head = ((head + (uint64_t(1) << 48)) & ~MASK) | (nxt & MASK);

            if (free_head_.compare_exchange_weak(head, new_head,
                    std::memory_order_acq_rel,
                    std::memory_order_acquire)) {
                T* ptr = slab_ + idx;
                new (ptr) T();
                used_.fetch_add(1, std::memory_order_relaxed);
                return ptr;
            }
        }
    }

    /// Return a slot to the pool.
    void release(T* ptr) noexcept {
        if (!ptr) return;
        assert(ptr >= slab_ && ptr < slab_ + Capacity);
        ptr->~T();
        const size_t idx = static_cast<size_t>(ptr - slab_);
        FreeNode& node = freelist_[idx];

        uint64_t head = free_head_.load(std::memory_order_relaxed);
        for (;;) {
            node.next.store(head & MASK, std::memory_order_relaxed);
            const uint64_t new_head = ((head + (uint64_t(1) << 48)) & ~MASK) | (idx & MASK);
            if (free_head_.compare_exchange_weak(head, new_head,
                    std::memory_order_acq_rel,
                    std::memory_order_acquire)) break;
        }
        used_.fetch_sub(1, std::memory_order_relaxed);
    }

    size_t used()      const noexcept { return used_.load(std::memory_order_relaxed); }
    size_t capacity()  const noexcept { return Capacity; }
    size_t available() const noexcept { return Capacity - used(); }

    T* slab_begin() const noexcept { return slab_; }

private:
    T*        slab_      = nullptr;
    FreeNode* freelist_  = nullptr;
    size_t    slab_bytes_= 0;
    std::atomic<uint64_t> free_head_{0};
    std::atomic<size_t>   used_{0};

    void alloc_slab() {
        slab_bytes_ = sizeof(T) * Capacity;

#if defined(__linux__)
        // Attempt 2MB hugepages for cache-friendly TLB footprint
        void* raw = mmap(nullptr, slab_bytes_,
                         PROT_READ | PROT_WRITE,
                         MAP_PRIVATE | MAP_ANONYMOUS | MAP_HUGETLB,
                         -1, 0);
        if (raw == MAP_FAILED) {
            // Fallback: normal anonymous pages
            raw = mmap(nullptr, slab_bytes_,
                       PROT_READ | PROT_WRITE,
                       MAP_PRIVATE | MAP_ANONYMOUS,
                       -1, 0);
        }
        if (raw == MAP_FAILED) throw std::bad_alloc();

        // Pin pages — swap-related latency spikes unacceptable on hot path
        mlock(raw, slab_bytes_);

#  ifdef ROBIN_HAS_NUMA
        // NUMA bind to node 0 (where the NIC sits in typical dual-socket rack)
        if (numa_available() >= 0) {
            struct bitmask* mask = numa_bitmask_alloc(numa_num_configured_nodes());
            numa_bitmask_setbit(mask, 0);
            mbind(raw, slab_bytes_, MPOL_BIND,
                  mask->maskp, mask->size + 1, 0);
            numa_bitmask_free(mask);
        }
#  endif  // ROBIN_HAS_NUMA

        slab_ = static_cast<T*>(raw);

#elif defined(_WIN32)
        slab_ = static_cast<T*>(_aligned_malloc(slab_bytes_, 64));
        if (!slab_) throw std::bad_alloc();
        // Best-effort lock — VirtualLock fails silently if SE_LOCK_MEMORY_NAME
        // privilege is not granted; callers should run as Administrator for HFT.
        VirtualLock(slab_, slab_bytes_);
#else
        if (posix_memalign(reinterpret_cast<void**>(&slab_), 64, slab_bytes_) != 0)
            throw std::bad_alloc();
#endif

        freelist_ = new FreeNode[Capacity];
    }

    void free_slab() noexcept {
#if defined(__linux__)
        if (slab_) munmap(slab_, slab_bytes_);
#elif defined(_WIN32)
        if (slab_) _aligned_free(slab_);
#else
        if (slab_) std::free(slab_);
#endif
        slab_ = nullptr;
    }

    void build_freelist() noexcept {
        for (size_t i = 0; i < Capacity - 1; ++i)
            freelist_[i].next.store(i + 1, std::memory_order_relaxed);
        freelist_[Capacity - 1].next.store(EMPTY, std::memory_order_relaxed);
        free_head_.store(0, std::memory_order_release);
    }
};

// ============================================================================
// prefault_pool(): pre-touches all slab pages at startup.
// Call once from main() so that page faults never occur on the hot path.
// ============================================================================
template<typename T, size_t Cap>
inline void prefault_pool(SlabAllocator<T, Cap>& pool) {
    std::vector<T*> tmp;
    tmp.reserve(Cap);
    // Touch every slot
    for (size_t i = 0; i < Cap; ++i) {
        T* p = pool.acquire();
        if (!p) break;
        // Force page-in with a volatile write
        *reinterpret_cast<volatile uint8_t*>(p) = 0;
        tmp.push_back(p);
    }
    for (T* p : tmp) pool.release(p);
}

// ============================================================================
// Global singleton pool helpers
// ============================================================================
template<typename Queue, size_t Cap = (1 << 20)>
class PooledQueue {
public:
    using Pool = SlabAllocator<Queue, Cap>;

    static Pool& global_pool() {
        static Pool instance;
        return instance;
    }

    PooledQueue() : ptr_(global_pool().acquire()) {
        if (!ptr_) throw std::runtime_error("OrderQueue pool exhausted");
    }
    ~PooledQueue() { if (ptr_) global_pool().release(ptr_); }

    PooledQueue(const PooledQueue&) = delete;
    PooledQueue& operator=(const PooledQueue&) = delete;
    PooledQueue(PooledQueue&& o) noexcept : ptr_(o.ptr_) { o.ptr_ = nullptr; }

    Queue*       get()       noexcept { return ptr_; }
    const Queue* get() const noexcept { return ptr_; }
    Queue*       operator->()       noexcept { return ptr_; }
    const Queue* operator->() const noexcept { return ptr_; }
    Queue&       operator*()        noexcept { return *ptr_; }
    Queue*       release_raw()      noexcept { Queue* p = ptr_; ptr_ = nullptr; return p; }

private:
    Queue* ptr_;
};

// Legacy MemoryPool alias kept for backward compatibility
template<typename T, size_t BlockCount>
using MemoryPool = SlabAllocator<T, BlockCount>;

}  // namespace execution
}  // namespace quantum
