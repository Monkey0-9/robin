#pragma once

#include <cstddef>
#include <new>
#include <utility>
#include <cstdlib>

namespace quantum {
namespace execution {

template <typename T, size_t BlockCount>
class MemoryPool {
public:
    MemoryPool() {
#if defined(_WIN32)
        storage_ = static_cast<StorageBlock*>(_aligned_malloc(BlockCount * sizeof(StorageBlock), alignof(StorageBlock)));
#else
        if (posix_memalign(reinterpret_cast<void**>(&storage_), alignof(StorageBlock), BlockCount * sizeof(StorageBlock)) != 0)
            storage_ = nullptr;
#endif
        free_stack_ = new size_t[BlockCount];
        for (size_t i = 0; i < BlockCount; ++i) free_stack_[i] = i;
        free_index_ = BlockCount;
    }

    ~MemoryPool() {
#if defined(_WIN32)
        if (storage_) _aligned_free(storage_);
#else
        std::free(storage_);
#endif
        delete[] free_stack_;
    }

    MemoryPool(const MemoryPool&) = delete;
    MemoryPool& operator=(const MemoryPool&) = delete;
    MemoryPool(MemoryPool&&) = delete;
    MemoryPool& operator=(MemoryPool&&) = delete;

    T* allocate() {
        if (free_index_ == 0 || !storage_) return nullptr;
        size_t block_idx = free_stack_[--free_index_];
        return reinterpret_cast<T*>(&storage_[block_idx]);
    }

    void deallocate(T* ptr) {
        if (!ptr || !storage_) return;
        size_t block_idx = static_cast<size_t>(
            reinterpret_cast<char*>(ptr) - reinterpret_cast<char*>(storage_)) / sizeof(StorageBlock);
        if (block_idx < BlockCount) free_stack_[free_index_++] = block_idx;
    }

private:
    struct alignas(alignof(T)) StorageBlock {
        char data[sizeof(T)];
    };

    StorageBlock* storage_;
    size_t* free_stack_;
    size_t free_index_;
};

} // namespace execution
} // namespace quantum
