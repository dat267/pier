# Reproduced, unresolved findings

The pin is pi v1.6.0 (`faac5858`) and the port still implements v1.5.2 behavior.
`just goldens` reports the drift below; each entry names the fixture and what
changed between the tags, so each can be ported or dropped on purpose.

## AI: Google's custom-fetch rejection is gone (`ai/testdata/google-wire`)

- Severity: medium. `packages/ai/src/api/google-generative-ai.ts` no longer throws for a caller-supplied `fetch`; it passes it to the client (`httpOptions.fetch`), which also keeps undici's header and body timeouts working.
- Evidence: the generator aborts with `expected an error`: its `developer-rejects-*` and `vertex-rejects-*` cases no longer error.
- Reproduce: `node --experimental-strip-types ai/testdata/google-wire/generate.mjs ./pi`.

## TUI: syntax highlighting emits merged spans and drops one markdown token (`coding/interactive/testdata/readtool_parity_golden.txt`, `writetool_parity_golden.txt`)

- Severity: low for behavior, high for parity: every highlighted row differs.
- Evidence: adjacent same-color spans are merged (`int<reset> { ` instead of `int<reset> { <reset>`), and a markdown heading in a skill file renders unstyled (`# skill` lost its color).
- Reproduce: `node --experimental-strip-types coding/interactive/testdata/regenerate.mjs ./pi`.
