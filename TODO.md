# Reproduced audit findings

## Sweep position

Whole-tree sweep for dead code, bad logic and over-engineered artifacts, stopped after five fixes:

- `bf928bc2` `tui`: `RequestRender` ignored `force`, so the external-editor, SIGCONT and reload paths kept a stale differential baseline.
- `cd5887e3` `coding`: timing namespaces printed alphabetically instead of in insertion order.
- `73ef9c4b` `coding`: `TruncateTailLines` and `emptyWindowToNil` were a test-only copy of `TruncateTail`'s window loop.
- `3e00d16a` `mcp`: `jsonUnmarshalOrdered` forwarded to `json.Unmarshal` under a name promising an ordered decode.
- `3ec84ca6` `ai`: `parsePartialString` returned an error that was always nil, behind two stacked doc comments.

Resume with the `unparam` list (35 findings, `go run mvdan.cc/unparam@latest -exported=false ./...`): unused parameters (`sendAnthropicThreadRequest.model`, `resumeSession.agentDir`, `applyModelCycle.ctx`, `findWordBackwardSegments.text`), always-nil error results (`mcpBlockToContent`, `composeAPIKeyAuth`, `applyMcpServerConfigBase`, `lastRequestHeaders`), and unused return values (`sweepTempOutputFiles`, `CacheWarmer.stopRun`, `killGate.runKill`). Dead chains that are documented and stay: `NestedCallRecorder` (D308), the MCP `OAuthCallbackServer`, and `tui.ParseKey` with its key-parsing corpus.
