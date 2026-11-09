# Reproduced audit findings

Full records: 2026-11-09-markdown-audit (markdown renderer slice).
Reference: pi v1.5.0 (f9a26365).

## Open

- **A439** (low) markdown: a table followed by a whitespace-only final line
  renders an extra blank line. Test: `tui/markdown_table_eol_test.go`.
- **A440** (medium) markdown: an ordered list starting at `0.` renders from 1,
  because the parsed start number is normalized and the renderer keys on `> 0`.
  Test: `tui/markdown_orderedstart_test.go`.
- **A441** (low) markdown: a fenced code block's info string keeps backslash
  escapes in the header and the highlighter language. Test:
  `tui/markdown_fenceinfo_test.go`.
