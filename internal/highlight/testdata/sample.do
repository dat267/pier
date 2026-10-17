* a comment
clear all
set obs 10

gen x = _n
gen y = x^2
replace y = y + 1 if x > 5

summarize x y
regress y x

local name "world"
display "Hello, `name'"
display abs(-3)
