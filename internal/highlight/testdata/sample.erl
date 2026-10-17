-module(main).
-export([main/0, add/2]).

main() ->
    io:format("Hello, ~s~n", ["world"]),
    add(1, 2).

add(A, B) ->
    A + B.

-record(person, {name, age}).
