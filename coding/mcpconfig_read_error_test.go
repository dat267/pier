package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 packages/coding-agent/src/extensions/mcp/config.ts
// readConfigFile records read failures in LoadedMcpConfig.errors.
func TestLoadMcpConfigReportsUnreadableFile(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "mcp.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	loaded := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir})
	if len(loaded.Errors) != 1 || !strings.HasPrefix(loaded.Errors[0], path+":") {
		t.Fatalf("errors = %q, want a read diagnostic for %s", loaded.Errors, path)
	}
}
