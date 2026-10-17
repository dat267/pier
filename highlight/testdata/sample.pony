actor Main
  new create(env: Env) =>
    let name: String = "world"
    env.out.print("Hello, " + name)

class Foo[A]
  fun apply(x: A): A => x
