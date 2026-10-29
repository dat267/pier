//go:build linux

package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// pi v1.4.0 packages/coding-agent/src/extensions/mcp/config.ts writes mcp.json
// with Node's default file mode; Pier protects configuration that may hold tokens.
func TestAddMcpServerConfigUsesPrivateFileMode(t *testing.T) {
	oldUmask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	for _, existing := range []bool{false, true} {
		name := "new-file"
		if existing {
			name = "existing-file"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			if existing {
				if err := os.WriteFile(path, []byte(`{"mcpServers":{"private-server":{"command":"old"}}}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			replaced, err := AddMcpServerConfig(path, "private-server", json.RawMessage(`{"url":"https://example.com","headers":{"Authorization":"Bearer secret"}}`))
			if err != nil {
				t.Fatal(err)
			}
			if replaced != existing {
				t.Fatalf("replaced = %v, want %v", replaced, existing)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("mcp config mode = %#o, want %#o", got, 0o600)
			}
		})
	}
}

// pi v1.4.0 packages/coding-agent/src/extensions/mcp/config.ts reads mcp.json
// from the agent directory; secure it before consuming global configuration.
func TestLoadMcpConfigHardensExistingAgentDir(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	_ = LoadMcpConfig(McpConfigLoadOptions{AgentDir: dir})
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("MCP agent directory mode = %#o, want %#o", got, 0o700)
	}
}
