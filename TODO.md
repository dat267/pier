# Reproduced, unresolved findings

## MCP resource and resource-link sizes reject numbers JavaScript accepts

- Severity: low. `mcp/protocol` `Resource.Size` and `ContentBlock.Size` are `*int64`, while pi v1.5.2 `packages/mcp/src/protocol/types.ts` and `content.ts` type `size` as `size?: number`.
- Failure: a server sending `"size": 1.5` or `"size": 1e20` makes `resources/list` fail with a JSON decode error, and a `tools/call` result carrying such a resource link fails `validateCallToolResult` with "Invalid MCP tools/call result". Upstream lists and renders both.
- Evidence: `json.Unmarshal` of `{"uri":"file:///x","name":"x","size":1.5}` into `protocol.Resource` reports `cannot unmarshal number 1.5 into Go struct field Resource.size of type int64`.
- Repair plan: decide between `*float64` and `json.Number` for both fields, then format through a JS number path (`FormatSize` currently takes int64 and is used across the tool output code). Add a decode test at the `mcp.Client.ListResources` boundary and one for a resource-link content block.
