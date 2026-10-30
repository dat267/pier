package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// pi v1.4.0 packages/coding-agent/src/extensions/mcp/config.ts readConfigFile
// parses JSON before iterating Object.entries, so duplicate object keys are
// collapsed to one final value and produce at most one entry error.
func TestLoadMcpConfigCollapsesDuplicateServerKeys(t *testing.T) {
	agentDir := t.TempDir()
	config := `{"mcpServers":{"srv":{"command":"first"},"srv":{"timeout":0}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir})
	if len(loaded.Errors) != 1 {
		t.Fatalf("errors = %v, want one error for the final duplicate value", loaded.Errors)
	}
	if len(loaded.Servers) != 0 {
		t.Fatalf("servers = %v, want invalid final value to be rejected", loaded.Servers)
	}
}
