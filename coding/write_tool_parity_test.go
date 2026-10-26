package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/tools/write.ts with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/write-tool/generate.mjs ./pi > coding/testdata/write-tool/golden.json
func TestWriteToolMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/write-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Content  string `json:"content"`
		Expected struct {
			Text    string          `json:"text"`
			Details json.RawMessage `json:"details"`
			Written *string         `json:"written"`
			Error   string          `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty write-tool corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("old\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			tool := CreateWriteTool(dir)
			params, err := json.Marshal(map[string]string{"path": fixture.Path, "content": fixture.Content})
			if err != nil {
				t.Fatal(err)
			}
			result, execErr := tool.Execute("call", params, context.Background(), nil)
			if fixture.Expected.Error != "" {
				if execErr == nil {
					t.Fatalf("expected error %q, got success", fixture.Expected.Error)
				}
				want := strings.ReplaceAll(fixture.Expected.Error, "<dir>", dir)
				if execErr.Error() != want {
					t.Fatalf("error = %q, want %q", execErr.Error(), want)
				}
				return
			}
			if execErr != nil {
				t.Fatalf("unexpected error: %v", execErr)
			}
			var text strings.Builder
			for _, block := range result.Content {
				if content, ok := block.(ai.TextContent); ok {
					text.WriteString(content.Text)
				}
			}
			if text.String() != fixture.Expected.Text {
				t.Fatalf("text = %q, want %q", text.String(), fixture.Expected.Text)
			}
			if fixture.Expected.Written != nil {
				written, err := os.ReadFile(filepath.Join(dir, fixture.Path))
				if err != nil {
					t.Fatal(err)
				}
				if string(written) != *fixture.Expected.Written {
					t.Fatalf("file = %q, want %q", string(written), *fixture.Expected.Written)
				}
			}
		})
	}
}

// D279: a directory target surfaces Go's os error, not Node's EISDIR wording.
func TestWriteToolDirectoryError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CreateWriteTool(dir).Execute("call", []byte(`{"path":"adir","content":"x"}`), context.Background(), nil)
	if err == nil {
		t.Fatal("writing over a directory succeeded")
	}
	message := err.Error()
	if !strings.Contains(message, "adir") || !strings.Contains(message, "directory") {
		t.Fatalf("error = %q, want Go's os error naming the directory", message)
	}
	if strings.HasPrefix(message, "EISDIR:") {
		t.Fatalf("error = %q, want the port's own wording (D279)", message)
	}
}
