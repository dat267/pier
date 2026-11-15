# Reproduced audit findings

## Sweep position

Whole-tree sweep for dead code, bad logic and over-engineered artifacts, stopped after twenty fixes.

### Fixes with a behavior change

- `bf928bc2` `tui`: `RequestRender` ignored `force`, so the external-editor, SIGCONT and reload paths kept a stale differential baseline.
- `cd5887e3` `coding`: timing namespaces printed alphabetically instead of in insertion order.
- `d722cde2` `coding`: the ParseArgs oracle found one undocumented intentional difference (`-e` warns), now D472; the parse itself matched the reference.

### Dead code removed

- `73ef9c4b` `coding`: `TruncateTailLines` and `emptyWindowToNil` were a test-only copy of `TruncateTail`'s window loop.
- `9ef0c316` `interactive`: `SetAutocompleteWrappers` was never called, so `AutocompleteWiring.wrappers` was always nil and its wrapper/trigger-character loop never ran; also `LastEscapeTimeMS` and `TruncateToVisualLinesStart` had no caller.

### Over-engineered artifacts removed

- `3e00d16a` `mcp`: `jsonUnmarshalOrdered` forwarded to `json.Unmarshal` under a name promising an ordered decode.
- `3ec84ca6` `ai`: `parsePartialString` returned an error that was always nil, behind two stacked doc comments.
- `122f750a` `ai`, `46834b83` `cmd`, `46f88f41` `coding`, `40e18857` `markdown`, `d0c235ba`/`f577e106` `uiblock`/`mcp/oauth`/`tui`: parameters and results no branch read (model-cycle `ctx`, `resumeSession.agentDir`, `skipIndent`'s budget, `listItemStart`'s content indent and `columnAt`, `parseHTTPURL`'s URL text, `requestRender.force`, and others).
- `91ecd863`, `c5372584`: always-nil error results (`ToModelContent`, `mcpBlockToContent`, `applyMcpServerConfigBase`, `composeAPIKeyAuth`, `lastRequestHeaders`).
- `8e7b436b` `coding`: unread results and always-true flags (`runKill`, `CacheWarmer.stop`/`stopRun`, `snapshot`, `orderedObject.set`, validator bounds, `ceilDiv`).
- `67950bc0` `goal`: `FoldGoal` tracked two counters that always held one value, beside a hand-rolled `maxInt64`.
- `240ce5a7` `rpcclient`: `processSignalName` forwarded to `coding.ProcessSignalName`.

### Oracles added

- `d722cde2`, `90b8230e`: `coding/testdata/cliargs` runs the reference's `parseArgs` over 223 argv vectors (103 curated, 120 seeded random) and compares the ported fields.

### Where to resume

The mechanical layers are clean: `unparam` reports nothing, `staticcheck -checks all` (minus the documented exclusions) reports nothing, `golangci-lint` reports nothing, and `deadcode`'s actionable subset is empty (every remaining unreachable function is referenced by a test or a same-file chain, i.e. the ported SDK surface). Duplication is confined to generated language tables, model-catalog literals and test files.

What remains is semantic reading of the large files no oracle covers, in this order: `coding/sdk.go` `CreateAgentSession` (551 lines), `coding/tools_bash.go` `CreateShellTool` (360), `coding/tools_search.go` `grep`/`find` (268/193), `coding/rpcmode.go` `handle` (261), `coding/interactive/app.go` `NewApp` (430) and `settingsselector.go` (415), `coding/bashexecutor.go` `ExecuteBashWithOperations` (211), and `mcp/oauth` `discovery.go`/`flow.go`. Areas read without a finding so far: `agent` loop, `ai` stream parsing, `mcp` transports, `tui` markdown, `coding/interactive` event runtime, `internal/offloop`, `internal/jsdiff`, `internal/goal`, `coding/rpcclient`, `coding/truncate`, `coding/compaction` cut, `coding/modelconfig`, `coding/cliargs`.

Documented dead chains that stay: `NestedCallRecorder` (D308), the MCP `OAuthCallbackServer`, and `tui.ParseKey` with its key-parsing corpus.
