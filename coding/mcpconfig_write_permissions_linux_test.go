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
