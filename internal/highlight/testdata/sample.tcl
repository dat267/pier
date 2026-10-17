# a comment
proc greet {name} {
    puts "Hello, $name"
    set msg [string toupper $name]
    return $msg
}
set x 10
if {$x > 5} {
    puts "big"
}
