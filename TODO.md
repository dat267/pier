# Reproduced audit findings

## A477 `FooterTruncate` returns dots when the text already fits

- Severity: low (visible footer truncation at narrow widths).
- Reference (pi v1.5.0 `f9a26365`, `packages/tui/src/utils.ts:1186`
  `truncateToWidth`, which `footer.ts` calls): when the ellipsis does not fit
  (`ellipsisWidth >= maxWidth`), a text that already fits is returned as-is.
- Port: `FooterTruncate` (`coding/interactive/footer.go:126`) short-circuits
  `maxWidth <= 3` to `"..."[:maxWidth]` without the fits-check.
- Measured:

  | text | width | reference | port |
  |---|---|---|---|
  | `ab` | 2 | `ab` | `..` |
  | `a` | 2 | `a` | `..` |
  | `a` | 1 | `a` | `.` |
  | `` (empty) | 2 | `` | `..` |
  | `abc` | 3 | `abc` | `...` |

  Cases where the text does **not** fit agree (`日本語` at 2 -> `..` in both).
- Reachable: `FooterTruncate` renders the footer's cwd basename at 25 columns
  and the joined status line at `min(80, width)`, so a narrow terminal can hit
  the `<= 3` branch.
- Cause: the port re-implements `truncateToWidth` instead of porting its
  ellipsis branch; `tui.TruncateToWidth` has the correct logic but emits ANSI
  resets, which a footer string cannot carry.
- Fix: apply the reference's fits-check before the `maxWidth <= 3`
  short-circuit.
- Verification: a table test over the fitting and non-fitting cases at widths
  1-4, plus a CJK case.
