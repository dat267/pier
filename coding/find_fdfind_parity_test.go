package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/tools/find.ts calls ensureTool("fd");
// utils/tools-manager.ts getToolPath tries systemBinaryNames ["fd", "fdfind"].
// Debian/Ubuntu install the same tool as fdfind, without an fd executable.
func TestFindToolUsesFdfindWhenFDUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable stubs")
	}
	for _, hasFD := range []bool{false, true} {
		name := "fdfind-only"
		if hasFD {
			name = "fd-preferred-control"
		}
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "fdfind"), []byte("#!/bin/sh\nprintf 'from-fdfind.txt\\n'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			want := "from-fdfind.txt"
			if hasFD {
				if err := os.WriteFile(filepath.Join(bin, "fd"), []byte("#!/bin/sh\nprintf 'from-fd.txt\\n'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				want = "from-fd.txt"
			}
			t.Setenv("PATH", bin)
			result, err := CreateFindTool(t.TempDir()).Execute("call", json.RawMessage(`{"pattern":"*.txt"}`), context.Background(), nil)
			if err != nil {
				t.Fatalf("find with available system binary: %v", err)
			}
			if len(result.Content) != 1 {
				t.Fatalf("content = %+v", result.Content)
			}
			text, ok := result.Content[0].(ai.TextContent)
			if !ok || text.Text != want {
				t.Fatalf("content = %+v, want %q", result.Content, want)
			}
		})
	}
}
