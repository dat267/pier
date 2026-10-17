.include "m328pdef.inc"

.org 0x0000
    rjmp main

main:
    ldi r16, 0xFF
    out DDRB, r16
loop:
    sbi PORTB, 5
    ldi r17, 0x0F
    add r16, r17
    cpi r16, 0x80
    brne loop
    rjmp main
