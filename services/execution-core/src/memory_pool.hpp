// services/execution-core/src/memory_pool.hpp
// Pre-allocated cache-aligned static memory pool to completely avoid heap usage in hot execution paths.

#pragma once

#include <cstddef>
#include <new>
#include <utility>

namespace quantum {
namespace execution {

template <typename T, size_t BlockCount>
class MemoryPool {
public:
    MemoryPool() {
        // Initialize free list linked index stack
        for (size_t i = 0; i < BlockCount; ++i) {
            free_stack_[i] = i;
        }
        free_index_ = BlockCount;
    }

    ~MemoryPool() = default;

    // Allocate memory block
    T* allocate() {
        if (free_index_ == 0) {
            return nullptr; // Pool exhausted
        }
        size_t block_idx = free_stack_[--free_index_];
        return reinterpret_cast<T*>(&storage_[block_idx]);
    }

    // Deallocate memory block
    void deallocate(T* ptr) {
        if (!ptr) return;
        
        size_t block_idx = (reinterpret_cast<char*>(ptr) - reinterpret_cast<char*>(storage_)) / sizeof(StorageBlock);
        if (block_idx < BlockCount) {
            free_stack_[free_index_++] = block_idx;
        }
    }

private:
    struct alignas(alignof(T)) StorageBlock {
        char data[sizeof(T)];
    };

    StorageBlock storage_[BlockCount];
    size_t free_stack_[BlockCount];
    size_t free_index_;
};

} // namespace execution
} // namespace quantum
