# Reproduced, unresolved findings

## The HTML export never fills `renderedTools`, so custom tool frames fall back to JSON

- Severity: low. `/export` output for an MCP or extension tool shows its raw arguments as JSON and its result as plain text, with no rendered call header, colours or clickable links.
- Evidence: `coding/interactive/exporthtml.go` reads `renderedTools` when building the page (`:577`) and defines `renderedToolHTML`, but nothing ever populates the map, so the template's fallback branch runs (`exporthtml/template.js:1139`). Upstream fills it through `export-html/tool-renderer.ts`, which renders each tool call and result with the TUI renderers and `ansiToHtml`; that is also where the v1.6.0 OSC 8 link conversion and the blank-line trim live, so the port's `AnsiToHTML`/`AnsiLinesToHTML` are reachable only from tests.
- Fix shape: render each tool call/result in `ExportSessionToHTML` through the session's renderers, convert with `AnsiToHTML`, and store the three variants in `RenderedTools`. Then the OSC 8 and blank-line behaviour that is already ported becomes live.
