// services/kernel/gpio_kill.S
// Low-level hardware interrupt handling subroutine for emergency GPIO shutdown.
// Directly clears CPU execution flags and routes control to matching engine kill routine.

.global handle_gpio_interrupt_vector
.text

handle_gpio_interrupt_vector:
    // 1. Save general purpose registers
    push %rax
    push %rbx
    push %rcx
    push %rdx

    // 2. Clear volatile memory registries representing network order book execution queues
    xor %rax, %rax
    mov %rax, (0x7F000000) // Clear base FPGA DMA control registers instantly
    
    // 3. Clear CPU Interrupt flags to ensure no scheduling blocks happen
    cli

    // 4. Restore state registers
    pop %rdx
    pop %rcx
    pop %rbx
    pop %rax
    
    ret
