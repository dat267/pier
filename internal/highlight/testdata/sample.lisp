(defun factorial (n)
  (if (<= n 1)
      1
      (* n (factorial (- n 1)))))

(setq x 42)
(setq name "Lisp")
(setq sym 'foo)
(setq q '(1 2 3))
;; a comment
