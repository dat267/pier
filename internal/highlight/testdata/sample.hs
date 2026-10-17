module Main (main) where

import Data.List (sort)

data Shape
  = Circle Double
  | Square Double

area :: Shape -> Double
area (Circle r) = pi * r * r
area (Square s) = s * s

main :: IO ()
main = print (sort [3, 1, 2])
