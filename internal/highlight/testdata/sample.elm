module Main exposing (main, add)

import Html exposing (text)

type Maybe a
    = Just a
    | Nothing

add : Int -> Int -> Int
add a b =
    a + b

record =
    { name = "Elm", count = 1 }

main =
    text "Hello"
