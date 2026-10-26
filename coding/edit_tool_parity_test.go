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

// pi v1.4.0 (4a080252), core/tools/edit.ts with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/edit-tool/generate.mjs ./pi > coding/testdata/edit-tool/golden.json
func TestEditToolMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/edit-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string          `json:"name"`
		File     string          `json:"file"`
		Edits    json.RawMessage `json:"edits"`
		Expected struct {
			Text    string          `json:"text"`
			Details json.RawMessage `json:"details"`
			File    string          `json:"file"`
			Error   string          `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty edit-tool corpus")
	}
	seed := map[string]string{
		"plain.txt":       "alpha\nbeta\ngamma\n",
		"crlf.txt":        "alpha\r\nbeta\r\ngamma\r\n",
		"bom.txt":         "\uFEFFalpha\nbeta\n",
		"multi.txt":       "one\ntwo\nthree\nfour\nfive\n",
		"duplicate.txt":   "a\na\n",
		"fuzzy.txt":       "  alpha   \n  beta\n",
		"no-trailing.txt": "alpha\nbeta",
	}
	tool := CreateEditTool(t.TempDir())
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			dir := t.TempDir()
			tool = CreateEditTool(dir)
			for name, content := range seed {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			params, err := json.Marshal(map[string]any{"path": fixture.File, "edits": fixture.Edits})
			if err != nil {
				t.Fatal(err)
			}
			result, execErr := tool.Execute("call", params, context.Background(), nil)
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
			written, err := os.ReadFile(filepath.Join(dir, fixture.File))
			if err != nil {
				t.Fatal(err)
			}
			if string(written) != fixture.Expected.File {
				t.Fatalf("file = %q, want %q", string(written), fixture.Expected.File)
			}
		})
	}
}
