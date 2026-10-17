" a comment
set number
let g:name = "world"

function! Greet(name)
    echo "Hello, " . a:name
endfunction

if exists('g:name')
    echo g:name
endif

let s:count = 42
