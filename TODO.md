# Reproduced audit findings

## P3

- **A414 (M259)**: Link reference definitions are not recognized. `'[x]: /u\n* a'` lexes as `def | ul` in marked, and the definition renders nothing. Pier lexes it as `p:"[x]: /u" | ul` and shows the definition line as text.
