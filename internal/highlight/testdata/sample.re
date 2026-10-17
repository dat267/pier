/* a comment */
type person = {
  name: string,
  age: int
};

let add = (a, b) => a + b;

let greet = (p: person) => {
  print_string("Hello, " ++ p.name)
};

let numbers = [1, 2, 3];

let result = switch (x) {
| Some(v) => v
| None => 0
};

module Util = {
  let double = (x) => x * 2;
};
