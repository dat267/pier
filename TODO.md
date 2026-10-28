# Reproduced audit findings

- CR-144: In-class `\S` outside the `[\s\S]` idiom keeps Go's ASCII complement, so classes such as `[a\Sb]` reject NBSP and `[^\S]` does not match ECMAScript whitespace.

- CR-145: Astral characters are matched as one rune instead of two UTF-16 code units, so dot/quantifiers/classes and escapes split them differently from highlight.js (24 of 9122 generated cases differed).

