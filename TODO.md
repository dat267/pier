# Reproduced audit findings

## Sweep position

Whole-tree sweep for dead code, bad logic and over-engineered artifacts, stopped after thirteen fixes:

- `bf928bc2` `tui`: `RequestRender` ignored `force`, so the external-editor, SIGCONT and reload paths kept a stale differential baseline.
- `cd5887e3` `coding`: timing namespaces printed alphabetically instead of in insertion order.
- `73ef9c4b` `coding`: `TruncateTailLines` and `emptyWindowToNil` were a test-only copy of `TruncateTail`'s window loop.
- `3e00d16a` `mcp`: `jsonUnmarshalOrdered` forwarded to `json.Unmarshal` under a name promising an ordered decode.
- `3ec84ca6` `ai`: `parsePartialString` returned an error that was always nil, behind two stacked doc comments.
- `122f750a` `ai`: `sendAnthropicThreadRequest` and `oauthRequiredString` carried parameters no branch read.
- `46834b83` `cmd`: `resumeSession` took an `agentDir` it never used.
- `91ecd863` `coding`: `ToModelContent`, `mcpBlockToContent` and `applyMcpServerConfigBase` returned always-nil errors.
- `c5372584` `coding`/`mcp`: `composeAPIKeyAuth` and `lastRequestHeaders` returned always-nil errors.
- `46f88f41` `coding`: seven internal functions took arguments nothing read (model-cycle `ctx`, `switchSession`/`sessionNew` `ctx`, and four stale parameters).
- `8e7b436b` `coding`: unread results and always-true flags (`runKill`, `CacheWarmer.stop`/`stopRun`, `snapshot`, `orderedObject.set`, validator bounds, `ceilDiv`).
- `40e18857` `markdown`: `skipIndent`'s constant budget, `listItemStart`'s unused content indent (and `columnAt`), `matchRefLink`'s unused start.
- `d0c235ba`/`f577e106`: six more unused arguments across `uiblock`, `mcp/oauth` and `tui`, plus `sweepTempOutputFiles`' unread counts and `readClipboardImageViaPngpaste`'s always-false flag.

`go run mvdan.cc/unparam@latest -exported=false ./...` now reports nothing. Resume with `go run golang.org/x/tools/cmd/deadcode@latest ./...` (531 unreachable functions, mostly the SDK surface the CLI never calls): the useful subset is exported functions whose only callers are tests. Dead chains that are documented and stay: `NestedCallRecorder` (D308), the MCP `OAuthCallbackServer`, and `tui.ParseKey` with its key-parsing corpus.
