parent(alice, bob).
parent(bob, carol).

ancestor(X, Y) :- parent(X, Y).
ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).

% a comment
greeting('Hello', "world").
