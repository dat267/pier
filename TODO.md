# Reproduced audit findings

## P1

- **A405 (M256)**: An unclosed fenced code block loses its newline-less last line, and `TrimPartialClosingFences` then subtracts that line's length from the body a second time: `~~~\nhello\n~~` renders `hel`, and `~~~\n~` panics with `slice bounds out of range [:-1]` inside `Markdown.Render`. Marked keeps the line (`token.text = "hello\n~~"`) and upstream trims it once, rendering `hello`.

## P3

- **A406 (M256)**: A fenced code block indented 1-3 spaces keeps that indent in its content lines, so `'  ```\n  foo\n  ```'` renders `"  foo"` where marked strips the fence's indent and renders `"foo"` (`' ```js\n  foo\n   bar\n ```'` keeps two and three spaces instead of one and two).
