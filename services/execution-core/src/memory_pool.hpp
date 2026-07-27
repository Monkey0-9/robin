#pragma once

#include <cstddef>
#include <new>
#include <utility>
#include <cstdlib>
#include <atomic>

namespace quantum {
namespace execution {

template <typename T, size_t BlockCount>
class MemoryPool {
public:
    static_assert(BlockCount < (1ULL << 48), "BlockCount too large");
    static constexpr uint64_t NULL_IDX = (1ULL << 48) - 1;

    MemoryPool() {
#if defined(_WIN32)
        storage_ = static_cast<Node*>(_aligned_malloc(BlockCount * sizeof(Node), alignof(Node)));
#else
        if (posix_memalign(reinterpret_cast<void**>(&storage_), alignof(Node), BlockCount * sizeof(Node)) != 0)
            storage_ = nullptr;
#endif
        if (storage_) {
            for (size_t i = 0; i < BlockCount - 1; ++i) {
                storage_[i].next_idx = i + 1;
            }
            storage_[BlockCount - 1].next_idx = NULL_IDX;
            head_.store(0, std::memory_order_relaxed); // ABA = 0, idx = 0
        }
    }

    ~MemoryPool() {
#if defined(_WIN32)
        if (storage_) _aligned_free(storage_);
#else
        std::free(storage_);
#endif
    }

    MemoryPool(const MemoryPool&) = delete;
    MemoryPool& operator=(const MemoryPool&) = delete;
    MemoryPool(MemoryPool&&) = delete;
    MemoryPool& operator=(MemoryPool&&) = delete;

    T* allocate() {
        if (!storage_) return nullptr;
        uint64_t head = head_.load(std::memory_order_acquire);
        while (true) {
            uint64_t idx = head & NULL_IDX;
            if (idx == NULL_IDX) return nullptr; // OOM

            uint64_t next_idx = storage_[idx].next_idx;
            uint64_t aba = (head >> 48) + 1;
            uint64_t new_head = (aba << 48) | next_idx;

            if (head_.compare_exchange_weak(head, new_head, std::memory_order_acq_rel, std::memory_order_acquire)) {
                return reinterpret_cast<T*>(&storage_[idx].data);
            }
        }
    }

    void deallocate(T* ptr) {
        if (!ptr || !storage_) return;
        size_t idx = static_cast<size_t>(
            reinterpret_cast<char*>(ptr) - reinterpret_cast<char*>(storage_)) / sizeof(Node);
        
        if (idx >= BlockCount) return;

        uint64_t head = head_.load(std::memory_order_acquire);
        while (true) {
            storage_[idx].next_idx = head & NULL_IDX;
            uint64_t aba = (head >> 48) + 1;
            uint64_t new_head = (aba << 48) | static_cast<uint64_t>(idx);
            
            if (head_.compare_exchange_weak(head, new_head, std::memory_order_acq_rel, std::memory_order_acquire)) {
                break;
            }
        }
    }

private:
    struct alignas(alignof(T)) Node {
        uint64_t next_idx;
        char data[sizeof(T)];
    };

    Node* storage_;
    std::atomic<uint64_t> head_;
};

} // namespace execution
} // namespace quantum
