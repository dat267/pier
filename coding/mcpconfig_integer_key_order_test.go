package coding

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// pi v1.4.0 packages/coding-agent/src/extensions/mcp/config.ts iterates
// mcpServers with Object.entries, which enumerates integer-index keys first.
func TestLoadMcpConfigOrdersIntegerServerKeysLikeJavaScript(t *testing.T) {
	agentDir := t.TempDir()
	config := `{"mcpServers":{"10":{"command":"ten"},"2":{"command":"two"},"1":{"command":"one"},"01":{"command":"leading-zero"},"4294967295":{"command":"max-uint32"},"4294967294":{"command":"last-index"},"0":{"command":"zero"}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir})
	if len(loaded.Errors) != 0 {
		t.Fatalf("errors = %v", loaded.Errors)
	}
	var names []string
	for _, server := range loaded.Servers {
		names = append(names, server.Name)
	}
	if want := []string{"0", "1", "2", "10", "4294967294", "01", "4294967295"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("server names = %v, want JavaScript property order %v", names, want)
	}
}
