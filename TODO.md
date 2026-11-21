# Reproduced, unresolved findings

The pin is pi v1.6.0 (`faac5858`) and the port still implements v1.5.2 behavior.
`just goldens` reports the drift below; each entry names the fixture and what
changed between the tags, so each can be ported or dropped on purpose.

## TUI: syntax-highlighted rows differ from the reference (`coding/interactive/testdata/readtool_parity_golden.txt`, `writetool_parity_golden.txt`)

- Severity: low for behavior, high for parity: every highlighted row differs, so the two families cannot be regenerated until it is resolved.
- Evidence: for `int { return 1 }` the reference emits
  `…mint\x1b[39m { \x1b[38;2;199;146;234mreturn\x1b[39m \x1b[38;2;247;140;108m1\x1b[39m }…`,
  while the port emits a reset pair around the unstyled run and re-opens the same color between a space and the next token:
  `…mint\x1b[39m\x1b[39m { \x1b[39m\x1b[38;2;199;146;234mreturn…`.
- Two causes to check when porting: pier's `highlightFormatter.apply` calls `theme.Fg("")` for a scope with no colour (emitting the extra reset pair), and adjacent same-colour runs are not coalesced. A markdown heading in a skill file also renders unstyled (`# skill`), which points at a theme scope that lost its colour.
- Reproduce: `node --experimental-strip-types coding/interactive/testdata/regenerate.mjs ./pi`.
