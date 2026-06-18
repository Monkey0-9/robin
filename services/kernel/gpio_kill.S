.section .text
.globl asm_kill_switch_handler
.type asm_kill_switch_handler, @function
.align 64

asm_kill_switch_handler:
    push    %rax
    push    %rcx
    push    %rdx
    push    %rsi
    push    %rdi
    push    %r8
    push    %r9
    push    %r10
    push    %r11

    rdtscp

    call    kill_switch_isr

    mfence

    pop     %r11
    pop     %r10
    pop     %r9
    pop     %r8
    pop     %rdi
    pop     %rsi
    pop     %rdx
    pop     %rcx
    pop     %rax

    ret

.size asm_kill_switch_handler, . - asm_kill_switch_handler
