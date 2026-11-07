# Reproduced audit findings

## P3

- **A409 (M257)**: Lists and blockquotes have no lazy continuation, so a paragraph line that follows a list or quote without a blank line becomes a separate block: `'- a\n- b\ntext'` renders `text` at column 0 where marked and CommonMark keep it in the last item, and `'> a\nb'` closes the quote.
