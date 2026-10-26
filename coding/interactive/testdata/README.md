# Interactive color fixtures

The built-in `../themes/dark.json` and `light.json` are pier's palette (D154),
not a copy of upstream's colors. They are the single data source for the CLI,
library fallback, HTML export, adaptive no-color signal panels and these fixtures.

From the repository root, using the existing offline reference dependencies:

```sh
just reference
node --experimental-strip-types coding/interactive/testdata/regenerate.mjs ./pi --write
node --experimental-strip-types coding/interactive/testdata/regenerate.mjs ./pi
```

The second command regenerates 15 golden families. The third checks every byte
without writing. Individual `gen-*.mjs` files write one family to standard output.
The loader rejects a reference HEAD other than pi v1.4.0 (`4a080252`). No command
installs dependencies or modifies `./pi`, its packages or its `node_modules`.
Node must support `registerHooks` and TypeScript stripping.

## Reference and adaptations

`golden-reference.mjs` resolves workspace imports to reference TypeScript in
memory, supplies the built-in pier palette directory to upstream `theme.ts`,
and uses the reference's vendored marked-cjk-friendly browser bundle as an ES
module. It isolates HOME and the agent directory in a temporary directory, pins
truecolor, disables images/hyperlinks and uses UTC+07 (matching `tzpin_test.go`).
Session/tree cases use a fixed clock; model refresh stays pending; loaders stop
before rendering, so animation and background refresh cannot race the fixture.
Temporary directories are removed on exit. None of the generators invoke Go or
read the expected golden files.

Most components, theme algorithms and all TUI primitives execute from v1.4.0.
Reconstructing the generators exposed older **non-color** contracts in existing
fixtures. This color migration deliberately does not change those behaviors:

- `first-time-setup.ts` executes from `d1230ea`, the revision cited by original
  port commit `790fa48`. This preserves the built-but-unwired dialog's detected
  appearance setup. The OAuth selector now executes from v1.4.0, including its
  `not configured` status wording (CR-051).
- The generic call-fallback method in `tool-execution.ts` comes from `d1230ea`
  (original port `acd62d7`); all other methods execute from v1.4.0. Thus the
  throwing-call-renderer case keeps its name-only fallback, without adding args.
- The settings fixture uses v1.4.0's renderer but excludes the session-share
  destination row and retains the existing `Automatic` display casing. The
  transformation is explicit in the loader, not a post-render golden rewrite.
- D94 omits the announcement image; the harness removes the Image component,
  leaving its surrounding spacers. D263's thinking-italic difference is still
  normalized only by its existing Go test, with its separate regression intact.
- The fullscreen-renderer fixture evaluates the pinned style expressions rather
  than starting a terminal. The edit fixture injects a completed preview from
  upstream `generateDiffString` into the upstream renderer. It does not touch
  the historical fixed `/tmp/pier_parity/f.txt` path or execute an edit.
- Theme detection entries keep their legacy projection shape while calling the
  current `terminalAppearance` and `detectColorFgBgTheme` helpers.

These exceptions are coverage limits, not evidence that the corresponding
non-color behavior matches v1.4.0. Review them separately before removing the
legacy adapters. The historical commit must exist in the read-only checkout.

Before the palette migration, the recreated generators reproduced every
existing label and decoded JSON value in all 14 labeled families, and every
byte of the edit-render fixture, with the old JSON palette. They now render
pier's colors. One extra independent user-message render also replaces an
inline Go-output capture in the single-copy regression. No golden is hand-edited.
