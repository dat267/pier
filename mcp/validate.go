package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dat267/pier/mcp/protocol"
)

// Validation keyed to upstream client.ts: invalid results become
// InvalidRequest McpErrors with these exact messages.

func invalid(message string) *protocol.McpError {
	return &protocol.McpError{Code: protocol.JSONRPCErrorCode.InvalidRequest, Msg: message}
}

// validateInitializeResult checks the `initialize` result shape.
func validateInitializeResult(raw json.RawMessage) (*protocol.InitializeResult, error) {
	var decoded struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
		ServerInfo      *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Instructions *string `json:"instructions"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	var capabilities map[string]any
	if err := json.Unmarshal(decoded.Capabilities, &capabilities); err != nil || capabilities == nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	if decoded.ProtocolVersion == "" || decoded.ServerInfo == nil || decoded.ServerInfo.Name == "" || decoded.ServerInfo.Version == "" {
		return nil, invalid("Invalid MCP initialize result")
	}
	var result protocol.InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	return &result, nil
}

// validateListPage checks one page of a paginated list; some servers end
// pagination with `null` or `""` instead of omitting the cursor.
func validateListPage(method, key string, raw json.RawMessage, isItem func(map[string]any) bool) (items []map[string]any, nextCursor *string, err error) {
	var page map[string]any
	if err := json.Unmarshal(raw, &page); err != nil || page == nil {
		return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s result", method))
	}
	rawItems, ok := page[key].([]any)
	if !ok {
		return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s result", method))
	}
	items = make([]map[string]any, 0, len(rawItems))
	for _, entry := range rawItems {
		item, ok := entry.(map[string]any)
		if !ok || !isItem(item) {
			return nil, nil, invalid(fmt.Sprintf("Invalid entry in MCP %s result", method))
		}
		items = append(items, item)
	}
	if raw, has := page["nextCursor"]; has && raw != nil {
		switch cursor := raw.(type) {
		case string:
			if cursor != "" {
				nextCursor = &cursor
			}
		default:
			return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s cursor", method))
		}
	}
	return items, nextCursor, nil
}

func isTool(item map[string]any) bool {
	name, ok := item["name"].(string)
	if !ok {
		return false
	}
	_ = name
	schema, ok := item["inputSchema"].(map[string]any)
	return ok && schema != nil
}

// isResource: `name` is required by the spec, but some servers omit it; the
// URI stands in.
func isResource(item map[string]any) bool {
	if _, ok := item["uri"].(string); !ok {
		return false
	}
	if _, has := item["name"]; !has {
		return true
	}
	_, ok := item["name"].(string)
	return ok
}

func isResourceTemplate(item map[string]any) bool {
	if _, ok := item["uriTemplate"].(string); !ok {
		return false
	}
	if _, has := item["name"]; !has {
		return true
	}
	_, ok := item["name"].(string)
	return ok
}

// decodeResource applies the missing-name fallback to the URI.
func decodeResource(item map[string]any) (protocol.Resource, error) {
	if _, has := item["name"]; !has {
		item = cloneMap(item)
		item["name"] = item["uri"]
	}
	return decodeItem[protocol.Resource](item)
}

func decodeResourceTemplate(item map[string]any) (protocol.ResourceTemplate, error) {
	if _, has := item["name"]; !has {
		item = cloneMap(item)
		item["name"] = item["uriTemplate"]
	}
	return decodeItem[protocol.ResourceTemplate](item)
}

// validateReadResourceResult checks a `resources/read` result.
func validateReadResourceResult(raw json.RawMessage) error {
	var page struct {
		Contents []json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(raw, &page); err != nil || page.Contents == nil {
		return invalid("Invalid MCP resources/read result")
	}
	for _, contents := range page.Contents {
		var entry map[string]any
		if err := json.Unmarshal(contents, &entry); err != nil || entry == nil {
			return invalid("Invalid contents in MCP resources/read result")
		}
		uri, uriOK := entry["uri"].(string)
		if !uriOK || uri == "" && false {
			return invalid("Invalid contents in MCP resources/read result")
		}
		if _, textOK := entry["text"].(string); textOK {
			continue
		}
		if _, blobOK := entry["blob"].(string); blobOK {
			continue
		}
		return invalid("Invalid contents in MCP resources/read result")
	}
	return nil
}

// validateCallToolResult checks a `tools/call` result; `content` is
// required by the spec, but servers that only return structuredContent omit
// it (the SDK defaults it too).
func validateCallToolResult(raw json.RawMessage) (*protocol.CallToolResult, error) {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
		return nil, invalid("Invalid MCP tools/call result")
	}
	if content, has := probe["content"]; has {
		if _, ok := content.([]any); !ok {
			return nil, invalid("Invalid MCP tools/call result")
		}
	}
	if structured, has := probe["structuredContent"]; has {
		object, ok := structured.(map[string]any)
		if !ok || object == nil {
			return nil, invalid("Invalid MCP tools/call structured content")
		}
	}
	var result protocol.CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, invalid("Invalid MCP tools/call result")
	}
	return &result, nil
}

func decodeItem[T any](item map[string]any) (T, error) {
	enc, err := json.Marshal(item)
	var decoded T
	if err != nil {
		return decoded, err
	}
	if err := json.Unmarshal(enc, &decoded); err != nil {
		return decoded, err
	}
	return decoded, nil
}

func cloneMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// listPage fetches and validates one page.
func (c *Client) listPage(ctx context.Context, method, key string, isItem func(map[string]any) bool, cursor *string, options RequestOptions) ([]map[string]any, *string, error) {
	var params any
	if cursor != nil {
		params = map[string]any{"cursor": *cursor}
	}
	raw, err := c.Request(ctx, method, params, options)
	if err != nil {
		return nil, nil, err
	}
	return validateListPage(method, key, raw, isItem)
}

// listAll walks a paginated list method to the end.
func (c *Client) listAll(ctx context.Context, method, key string, isItem func(map[string]any) bool, options RequestOptions) ([]map[string]any, error) {
	var items []map[string]any
	cursors := map[string]bool{}
	var cursor *string
	for pageNumber := 0; pageNumber < MaxListPages; pageNumber++ {
		pageItems, nextCursor, err := c.listPage(ctx, method, key, isItem, cursor, options)
		if err != nil {
			return nil, err
		}
		items = append(items, pageItems...)
		if nextCursor == nil {
			return items, nil
		}
		if cursors[*nextCursor] {
			return nil, fmt.Errorf("MCP %s returned duplicate cursor: %s", method, *nextCursor)
		}
		cursors[*nextCursor] = true
		cursor = nextCursor
	}
	return nil, fmt.Errorf("MCP %s exceeded %d pages", method, MaxListPages)
}
