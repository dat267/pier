package coding

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// TestMcpToolNamesIgnoreToolListOrder pins that a name collision inside one
// server hashes both tools, not only the later one. Upstream computes the
// collisions first ([...new Set(connection.tools.map(t => t.name))].map(...))
// and treats a candidate as taken when `plain.indexOf(candidate) !==
// plain.lastIndexOf(candidate)`, so "which one would keep the plain name does
// not depend on the order of the list"
// (extensions/mcp/index.ts registerTools, pi v1.5.0 f9a26365).
func TestMcpToolNamesIgnoreToolListOrder(t *testing.T) {
	direct := McpExposureDirect
	namesFor := func(tools []protocol.Tool) []string {
		t.Helper()
		config := LoadedMcpConfig{Servers: []McpServerEntry{
			{Name: "srv", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
		}}
		manager := NewMcpManager(context.Background(), McpManagerOptions{
			Config: config,
			CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
				_, client := newMCPFakeServerTools(t, entry.Name, tools)
				return client, nil
			},
		})
		defer func() { _ = manager.Close(context.Background()) }()
		names := make([]string, 0, len(tools))
		for _, tool := range manager.DirectTools() {
			names = append(names, tool.Name)
		}
		return names
	}

	// "a.b" and "a_b" sanitize to the same provider name.
	forward := namesFor([]protocol.Tool{{Name: "a.b"}, {Name: "a_b"}})
	reversed := namesFor([]protocol.Tool{{Name: "a_b"}, {Name: "a.b"}})
	if len(forward) != 2 || len(reversed) != 2 {
		t.Fatalf("tools = %v / %v", forward, reversed)
	}
	for _, name := range forward {
		if !strings.HasSuffix(name, "_") && !isHashedMcpToolName(name) {
			t.Errorf("name %q kept the plain name; both colliding tools must be hashed", name)
		}
	}
	if forward[0] != reversed[1] || forward[1] != reversed[0] {
		t.Errorf("names depend on order: forward %v, reversed %v", forward, reversed)
	}
}

// isHashedMcpToolName reports the `_<8 hex>` suffix createMcpToolName appends.
func isHashedMcpToolName(name string) bool {
	if len(name) < 9 || name[len(name)-9] != '_' {
		return false
	}
	for _, r := range name[len(name)-8:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
