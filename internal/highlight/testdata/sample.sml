structure Main = struct
  datatype shape = Circle of real | Square of real

  fun area (Circle r) = 3.14159 * r * r
    | area (Square s) = s * s

  val x = 42
  val name = "SML"
  val eq = 1 = 1
end
