package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.4.0 packages/mcp/src/client.ts validateListPage returns the
// original parsed tool objects, preserving inputSchema property enumeration
// order rather than rebuilding the object through an unordered map.
func TestClientListToolsPreservesInputSchemaPropertyOrder(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("tools/list", func(*protocol.JsonRpcRequest) (any, error) {
		return json.RawMessage(`{"tools":[{"name":"ordered","inputSchema":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}}}}]}`), nil
	})

	tools, err := client.ListTools(context.Background(), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}}}`
	if len(tools) != 1 || string(tools[0].InputSchema) != want {
		if len(tools) == 1 {
			t.Fatalf("input schema = %s, want %s", tools[0].InputSchema, want)
		}
		t.Fatalf("tools = %+v, want one ordered tool", tools)
	}
}
