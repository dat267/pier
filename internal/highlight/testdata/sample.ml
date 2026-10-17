let rec fact n =
  if n <= 1 then 1 else n * fact (n - 1)

type shape = Circle of float | Square of float

let area = function
  | Circle r -> 3.14 *. r *. r
  | Square s -> s *. s

let () = print_int (fact 5)
