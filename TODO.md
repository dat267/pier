# Reproduced audit findings

Full records: 2026-11-09-markdown-audit (markdown renderer slice).
Reference: pi v1.5.0 (f9a26365).

## Open

- **A441** (low) markdown: a fenced code block's info string keeps backslash
  escapes in the header and the highlighter language. Test:
  `tui/markdown_fenceinfo_test.go`.
