package coding

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), core/tools/find.ts resolves ensureTool("fd") before
// spawning and never stats the search path, so fd's own error (or the
// unavailable-tool error) is what the caller sees.
func TestFindToolDefersMissingPathToFD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX PATH without fd")
	}
	t.Run("unavailable", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := CreateFindTool(t.TempDir()).Execute("call",
			json.RawMessage(`{"pattern":"*.txt","path":"missing"}`), context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "fd is not available and could not be downloaded") {
			t.Fatalf("missing path without fd = %v, want the unavailable-tool error", err)
		}
	})
	t.Run("installed", func(t *testing.T) {
		requireBinary(t, "fd")
		dir := t.TempDir()
		_, err := CreateFindTool(dir).Execute("call",
			json.RawMessage(`{"pattern":"*.txt","path":"missing"}`), context.Background(), nil)
		if err == nil {
			t.Fatal("missing search path succeeded")
		}
		if strings.Contains(err.Error(), "Path not found:") {
			t.Fatalf("precheck still wins over fd: %v", err)
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Fatalf("fd error lost the path: %v", err)
		}
	})
}
