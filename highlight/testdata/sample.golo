module hello

function main = |args| {
  println("Hello, " + args: get(0))
  let x = 42
  foreach a in [1, 2, 3] {
    when a > 1 {
      print(a)
    } otherwise {
      print("small")
    }
  }
}
