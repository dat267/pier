# Reproduced audit findings

## CR-049: low embedded upstream dark/light palettes are stale by a palette redesign

- Category and affected paths or functions: parity/data,
  `coding/interactive/themes/dark.json` and `light.json`, consumed by
  `loadTheme`/`loadThemeJSON` when the pier palette installer is absent, by
  library/SDK callers, by HTML export without the registry, and by the
  interactive parity goldens (which clear the registry, per D154).
- Scenario: resolve the built-in upstream `dark` or `light` theme without the
  pier overrides and compare with pi v1.4.0. Expected: the pinned
  `modes/interactive/theme/{dark,light}.json`, whose vars are `okhsl(...)`
  identities (dark accent is `violet`, text `okhsl(234 3% 89%)`, background
  tokens such as `blueBg`) and which carry an `appearance` field. Actual: the
  embedded files are an older pi palette (`accent: #8abeb7`, `text: #d4d4d4`,
  `#343541` user panels), with no `appearance` field and a different token set.
- Impact: a caller that does not install pier's registered palette (D154) gets
  the old colors rather than pi v1.4.0's, and the "upstream palette" goldens
  and tests (`piertheme_test.go`, `components_test.go`) pin the stale values.
  The CLI is unaffected because `installPierTheme` shadows both names.
- Evidence: `diff coding/interactive/themes/dark.json
  pi/.../theme/dark.json` shows the missing `appearance` field and a fully
  different `vars`/`colors` set; the pin's banner is
  `okhsl(...)`, the port's `#rrggbb`. Copying the pinned files makes 15 golden
  and factory tests fail, because those goldens were generated from the stale
  palette.
- Authority: `pi/packages/coding-agent/src/modes/interactive/theme/dark.json`
  and `light.json` at pi v1.4.0 (4a080252); D154 describes the embedded pair as
  the upstream palettes for parity goldens.
- Root cause: the embedded upstream JSON was never refreshed after upstream's
  okhsl palette redesign, and no generator is committed for the component
  goldens that encode it.
- Repair plan: copy the pinned JSONs, regenerate the interactive component,
  selector, diff, exec, message, auth, settings, tree, session, model-selector,
  custom-editor, easter-egg and theme goldens from the pinned components (their
  generators are not in the repository and must be recreated), and update the
  two tests that assert the old palette. This is a wide, golden-regenerating
  change, so it is recorded here instead of being applied without the owner's
  agreement.
- Verification (when repaired): the copied files must be byte-identical to the
  pin, `./coding/interactive`, `./coding`, `just check`, `just cross`,
  `just reference`.
