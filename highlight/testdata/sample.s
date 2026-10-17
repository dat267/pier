    .text
    .global main
main:
    mov r0, #1
    add r0, r0, #2
    cmp r0, #3
    beq done
    b main
done:
    bx lr
