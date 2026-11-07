# Reproduced audit findings

## P3

- **A410 (M259)**: A blockquote absorbs the non-`>` lines that follow it into its own token list even when the paragraph rule stops, unless the quote's last token is a code block: `'> a\n| x |\n| - |'` yields `quote[p,table]` and `'> a\n<br>'` yields `quote[p,html]`, where pier closes the quote. Needs the reference's post-pass (`n.length` loop with code/blockquote/list handling).
- **A412 (M259)**: Two blank lines after a list item end the list, but the reference keeps it: `'- a\n\n- b'` is one list in both, `'- a\n\n\n- b'` is one list in marked (`items[0]` carries a `space` token) and `list | space | list` in pier. A list separated by two blank lines from its next item splits in two.
