# Reproduced audit findings

No reproduced finding is open. Resolved ones live in `git log`; the sweep position is below.

## Sweep position

Open: semantic reading of the large files no oracle covers, in this order: `coding/sdk.go` `CreateAgentSession` (551 lines), `coding/interactive/app.go` `NewApp` (430) and `settingsselector.go` (415), and `mcp/oauth` `discovery.go`'s fetch loop with `flow.go`'s client registration and step-up paths.

Clean mechanical layers: `go run mvdan.cc/unparam@latest -exported=false ./...` reports nothing, `staticcheck -checks all` (minus the documented exclusions in `.golangci.yml`) reports nothing, `golangci-lint` reports nothing, and `deadcode`'s actionable subset is empty (every remaining unreachable function is referenced by a test or a same-file chain, i.e. the ported SDK surface). Duplication is confined to generated language tables, model-catalog literals and test files.

Read without a finding: `agent` loop, `ai` stream parsing, `mcp` transports, `tui` markdown, `coding/interactive` event runtime, `internal/offloop`, `internal/jsdiff`, `internal/goal`, `coding/rpcclient`, `coding/truncate`, `coding/compaction` cut, `coding/modelconfig`, `coding/cliargs`, `coding/tools_search`, `coding/tools_bash` `CreateShellTool`, `coding/rpcmode`, `coding/bashexecutor`, `mcp/oauth/provider`, and `mcp/oauth/flow`'s discovery URLs, `secureEndpoint`, client-auth selection, token request and refresh.

Dead chains kept on purpose, not open work: `NestedCallRecorder` (D308), the MCP `OAuthCallbackServer`, and `tui.ParseKey` with its key-parsing corpus.
