package coding

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// SaveMcpToTempFile returns the path it wrote on success, with private
// permissions (utils/mcp saveToTempFile). Flipped success checks would return an
// empty path with a nil error.
func TestSaveMcpToTempFile(t *testing.T) {
	path, err := SaveMcpToTempFile([]byte("image-bytes"), ".png")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("no path returned for a successful write")
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "image-bytes" {
		t.Fatalf("file = %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
		}
	}
}

// CreateMcpToolName keeps the prefix plus a hash suffix within the length limit,
// and hashes a taken name (sanitizing can collide two tools).
func TestCreateMcpToolNameShortening(t *testing.T) {
	long := strings.Repeat("server-", 8)
	name := CreateMcpToolName(long, strings.Repeat("tool-", 8), func(string) bool { return false })
	if len(name) > MaxMcpToolNameLength {
		t.Fatalf("name %q is %d characters, limit %d", name, len(name), MaxMcpToolNameLength)
	}
	if name == "" || !strings.HasPrefix(name, "mcp__") {
		t.Fatalf("name = %q", name)
	}
	taken := CreateMcpToolName("s", "t", func(string) bool { return true })
	if taken == CreateMcpToolName("s", "t", func(string) bool { return false }) {
		t.Fatalf("a taken name was not hashed: %q", taken)
	}
}
