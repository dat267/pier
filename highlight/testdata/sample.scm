; a comment
#| block comment |#

(define (square x)
  (* x x))

(define (fact n)
  (if (<= n 1)
      1
      (* n (fact (- n 1)))))

(display "Hello, world")
(newline)

(let ((x 10) (y 20))
  (+ x y))

(car '(1 2 3))
