/* x86-64 assembly-level interrupt handler for hardware kill switch */
/* Called directly from GPIO interrupt - < 50ns entry to handler */
/* Preserves: rax, rcx, rdx, rsi, rdi, r8-r11 (caller-saved) */

.section .text
.globl asm_kill_switch_handler
.type asm_kill_switch_handler, @function
.align 64

asm_kill_switch_handler:
    /* Prologue: save minimal registers */
    push    %rax
    push    %rcx
    push    %rdx
    push    %rsi
    push    %rdi
    push    %r8
    push    %r9
    push    %r10
    push    %r11

    /* Get kernel GS base */
    /* RDTSCP for precise timestamp */
    rdtscp
    /* rax: low 32, rdx: high 32, rcx: processor ID */

    /* Call the C handler */
    call    kill_switch_isr

    /* MFENCE to ensure stores are visible */
    mfence

    /* Epilogue: restore registers */
    pop     %r11
    pop     %r10
    pop     %r9
    pop     %r8
    pop     %rdi
    pop     %rsi
    pop     %rdx
    pop     %rcx
    pop     %rax

    iretq

.size asm_kill_switch_handler, . - asm_kill_switch_handler
