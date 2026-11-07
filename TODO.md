# Reproduced audit findings

## P3

- **A413 (M259)**: A type-7 HTML start (any open or closing tag, such as `<br>`) interrupts a paragraph in pier, but in marked only the type 1-6 starts do: `'a\n<br>'` is `p:"a\n<br>"` in marked and `p:"a" | html` in pier. The quote chunk loop already uses the narrower rule (`quoteHTMLInterrupt`); `interruptsParagraph` does not.
- **A414 (M259)**: Link reference definitions are not recognized. `'[x]: /u\n* a'` lexes as `def | ul` in marked, and the definition renders nothing. Pier lexes it as `p:"[x]: /u" | ul` and shows the definition line as text.
- **A415 (M259)**: A setext underline after a line that marked reads as a table row or a list start makes a heading in pier: `'| - |\n===\n'` is `p:"| - |\n==="` in marked and `h1:"| - |"` in pier, and `'| x |\n10. a\n---'` is `p | ol | hr` in marked and `h2:"| x |\n10. a"` in pier.
- **A416 (M259)**: Indented code keeps or drops a trailing newline differently from marked when blank or whitespace-only lines follow it: `'    code\n \n'` is `code:"code" | space` in marked and `code:"code\n"` in pier, and `'\t- t\n '` is `code:"- t\n"` in marked and `code:"- t"` in pier.
