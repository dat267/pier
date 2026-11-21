# Reproduced, unresolved findings

## The HTML export never fills `renderedTools`, so custom tool frames fall back to JSON

- Severity: low. `/export` output for an MCP or extension tool shows its raw arguments as JSON and its result as plain text, with no rendered call header, colours or clickable links.
- Evidence: `coding/interactive/exporthtml.go` reads `renderedTools` when building the page (`:577`) and defines `renderedToolHTML`, but nothing ever populates the map, so the template's fallback branch runs (`exporthtml/template.js:1139`). Upstream fills it through `export-html/tool-renderer.ts`, which renders each tool call and result with the TUI renderers and `ansiToHtml`; that is also where the v1.6.0 OSC 8 link conversion and the blank-line trim live, so the port's `AnsiToHTML`/`AnsiLinesToHTML` are reachable only from tests.
- Fix shape: render each tool call/result in `ExportSessionToHTML` through the session's renderers, convert with `AnsiToHTML`, and store the three variants in `RenderedTools`. Then the OSC 8 and blank-line behaviour that is already ported becomes live.

## A replaced session keeps the first project's model scope

- Severity: low. The model cycle scope and its diagnostics belong to the cwd a session was resolved in.
- Evidence: `cmd/rpcmode.go:84` (`rpcSessionReplacer`) rebinds the project settings to the replacement's cwd but copies `base.ScopedModels` from the initial `run`, and `coding/sdk.go` uses `options.ScopedModels` as given. Upstream builds the scope inside the per-session factory, after the settings rebind, so `/resume` or `/fork` into another project resolves that project's `enabledModels` (`model-scope-session-replacement.test.ts`, v1.6.0).
- Fix shape: when the scope came from settings rather than from `--models`, re-run `scopedModelsFor` in the replacer after `settings.RebindProject`.
- Reproduce: start RPC with an `enabledModels` list, `switch_session` to a session whose project sets a different list, then inspect the cycle scope.
