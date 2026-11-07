# Reproduced audit findings

## P3

- **A414 (M259)**: Link reference definitions are not recognized. `'[x]: /u\n* a'` lexes as `def | ul` in marked, and the definition renders nothing. Pier lexes it as `p:"[x]: /u" | ul` and shows the definition line as text.- **A416 (M259)**: Indented code keeps or drops a trailing newline differently from marked when blank or whitespace-only lines follow it: `'    code\n \n'` is `code:"code" | space` in marked and `code:"code\n"` in pier, and `'\t- t\n '` is `code:"- t\n"` in marked and `code:"- t"` in pier.