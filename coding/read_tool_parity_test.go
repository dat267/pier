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

// pi v1.4.0 (4a080252), core/tools/read.ts text path with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/read-tool/generate.mjs ./pi > coding/testdata/read-tool/golden.json
func TestReadToolMatchesUpstreamTextPath(t *testing.T) {
	raw, err := os.ReadFile("testdata/read-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Offset   *float64 `json:"offset"`
		Limit    *float64 `json:"limit"`
		Expected struct {
			Text    string          `json:"text"`
			Details json.RawMessage `json:"details"`
			Error   string          `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty read-tool corpus")
	}
	dir := t.TempDir()
	files := map[string][]byte{
		"empty.txt":                        {},
		"three.txt":                        []byte("one\ntwo\nthree\n"),
		"no-newline.txt":                   []byte("one\ntwo"),
		"blank-lines.txt":                  []byte("a\n\n\nb\n"),
		"crlf.txt":                         []byte("a\r\nb\r\n"),
		"bom.txt":                          []byte("\uFEFFfirst\nsecond\n"),
		"invalid.txt":                      {0xff, 0x0a, 'A', 0x0a},
		"long-line.txt":                    []byte(strings.Repeat("x", 60000) + "\nsecond\n"),
		"many-lines.txt":                   []byte(manyLines(2500)),
		filepath.Join("sub", "nested.txt"): []byte("nested\n"),
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tool := CreateReadTool(dir, nil)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			params := map[string]any{"path": fixture.Path}
			if fixture.Offset != nil {
				params["offset"] = *fixture.Offset
			}
			if fixture.Limit != nil {
				params["limit"] = *fixture.Limit
			}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			result, execErr := tool.Execute("call", encoded, context.Background(), nil)
			if fixture.Expected.Error != "" {
				if execErr == nil {
					t.Fatalf("expected error %q, got success", fixture.Expected.Error)
				}
				if execErr.Error() != fixture.Expected.Error {
					t.Fatalf("error = %q, want %q", execErr.Error(), fixture.Expected.Error)
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
				t.Fatalf("text differs\n got: %q\nwant: %q", text.String(), fixture.Expected.Text)
			}
			gotDetails, err := json.Marshal(canonicalReadJSON(t, result.Details))
			if err != nil {
				t.Fatal(err)
			}
			wantDetails, err := json.Marshal(canonicalReadJSON(t, fixture.Expected.Details))
			if err != nil {
				t.Fatal(err)
			}
			if string(gotDetails) != string(wantDetails) {
				t.Fatalf("details = %s, want %s", gotDetails, wantDetails)
			}
		})
	}
}

func TestReadToolMissingFileError(t *testing.T) {
	dir := t.TempDir()
	_, err := CreateReadTool(dir, nil).Execute("call", []byte(`{"path":"nope.txt"}`), context.Background(), nil)
	if err == nil {
		t.Fatal("reading a missing file succeeded")
	}
	message := err.Error()
	if !strings.Contains(message, "no such file or directory") || !strings.Contains(message, "nope.txt") {
		t.Fatalf("error = %q, want Go's os error naming the path", message)
	}
	if strings.HasPrefix(message, "ENOENT:") {
		t.Fatalf("error = %q, want the port's own wording (D279)", message)
	}
}
