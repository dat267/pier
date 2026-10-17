module Demo

type Shape =
    | Circle of float
    | Rectangle of float * float

let area shape =
    match shape with
    | Circle r -> System.Math.PI * r * r
    | Rectangle (w, h) -> w * h

let f x =
    return! [ for i in 1 .. x -> i * i ]
