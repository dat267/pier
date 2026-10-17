import std/strformat

type
  Point = object
    x, y: float

proc `+`(a, b: Point): Point =
  Point(x: a.x + b.x, y: a.y + b.y)

let p = Point(x: 1.0, y: 2.0)
echo &"{p.x}, {p.y}"
let n = 0x1F'u8
