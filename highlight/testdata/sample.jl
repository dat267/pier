# a comment
function add(a::Int, b::Int)
    return a + b
end

struct Point
    x::Float64
    y::Float64
end

mutable struct Counter
    n::Int
end

name = "world"
println("Hello, $name")
s = "value: $(1 + 2)"
arr = [1, 2, 3]
d = Dict(:a => 1)
c = 'x'
cmd = `ls -la`
@time add(1, 2)
