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
	// client.ts checks presence and `typeof`; empty strings are valid, while
	// null is not a string. Pointers distinguish a missing member from "".
	var decoded struct {
		ProtocolVersion *string         `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
		ServerInfo      *struct {
			Name    *string `json:"name"`
			Version *string `json:"version"`
		} `json:"serverInfo"`
		Instructions json.RawMessage `json:"instructions"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	var capabilities map[string]any
	if err := json.Unmarshal(decoded.Capabilities, &capabilities); err != nil || capabilities == nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	if decoded.ProtocolVersion == nil || decoded.ServerInfo == nil || decoded.ServerInfo.Name == nil || decoded.ServerInfo.Version == nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	if len(decoded.Instructions) > 0 {
		// Unmarshalling null into a string is a silent no-op, so keep the
		// pointer to reject it: `typeof null !== "string"` upstream.
		var instructions *string
		if err := json.Unmarshal(decoded.Instructions, &instructions); err != nil || instructions == nil {
			return nil, invalid("Invalid MCP initialize result")
		}
	}
	var result protocol.InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, invalid("Invalid MCP initialize result")
	}
	return &result, nil
}

type validatedListItem struct {
	Raw    json.RawMessage
	Object map[string]any
}

// validateListPage checks one page of a paginated list; some servers end
// pagination with `null` or `""` instead of omitting the cursor. Raw item
// values are retained so nested JSON schemas keep JavaScript property order.
func validateListPage(method, key string, raw json.RawMessage, isItem func(map[string]any) bool) (items []validatedListItem, nextCursor *string, err error) {
	var page map[string]json.RawMessage
	if err := json.Unmarshal(raw, &page); err != nil || page == nil {
		return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s result", method))
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(page[key], &rawItems); err != nil || rawItems == nil {
		return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s result", method))
	}
	items = make([]validatedListItem, 0, len(rawItems))
	for _, rawItem := range rawItems {
		var item map[string]any
		if err := json.Unmarshal(rawItem, &item); err != nil || item == nil || !isItem(item) {
			return nil, nil, invalid(fmt.Sprintf("Invalid entry in MCP %s result", method))
		}
		items = append(items, validatedListItem{Raw: rawItem, Object: item})
	}
	if rawCursor, has := page["nextCursor"]; has {
		var value any
		if err := json.Unmarshal(rawCursor, &value); err != nil {
			return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s cursor", method))
		}
		if value != nil {
			cursor, ok := value.(string)
			if !ok {
				return nil, nil, invalid(fmt.Sprintf("Invalid MCP %s cursor", method))
			}
			if cursor != "" {
				nextCursor = &cursor
			}
		}
	}
	return items, nextCursor, nil
}

func isTool(item map[string]any) bool {
	if _, ok := item["name"].(string); !ok {
		return false
	}
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
func decodeResource(item validatedListItem) (protocol.Resource, error) {
	var resource protocol.Resource
	if err := json.Unmarshal(item.Raw, &resource); err != nil {
		return protocol.Resource{}, err
	}
	if _, has := item.Object["name"]; !has {
		resource.Name = item.Object["uri"].(string)
	}
	return resource, nil
}

func decodeResourceTemplate(item validatedListItem) (protocol.ResourceTemplate, error) {
	var template protocol.ResourceTemplate
	if err := json.Unmarshal(item.Raw, &template); err != nil {
		return protocol.ResourceTemplate{}, err
	}
	if _, has := item.Object["name"]; !has {
		template.Name = item.Object["uriTemplate"].(string)
	}
	return template, nil
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
		if _, uriOK := entry["uri"].(string); !uriOK {
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
	var result protocol.CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, invalid("Invalid MCP tools/call result")
	}
	if result.Content == nil {
		result.Content = []protocol.ContentBlock{}
	}
	return &result, nil
}

// listPage fetches and validates one page.
func (c *Client) listPage(ctx context.Context, method, key string, isItem func(map[string]any) bool, cursor *string, options RequestOptions) ([]validatedListItem, *string, error) {
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
func (c *Client) listAll(ctx context.Context, method, key string, isItem func(map[string]any) bool, options RequestOptions) ([]validatedListItem, error) {
	var items []validatedListItem
	cursors := map[string]bool{}
	var cursor *string
	for range MaxListPages {
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
