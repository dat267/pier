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

// pi v1.4.0 (4a080252), core/tools/ls.ts with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/ls-tool/generate.mjs ./pi > coding/testdata/ls-tool/golden.json
func TestLsToolMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/ls-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string   `json:"name"`
		Path     *string  `json:"path"`
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
		t.Fatal("empty ls-tool corpus")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tree", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "B.txt", "b.txt", "_x", "-y", ".hidden", "Z", "ä", "10", "9", "a b"} {
		if err := os.WriteFile(filepath.Join(dir, "tree", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "sub", "nested.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool := CreateLsTool(dir)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			params := map[string]any{}
			if fixture.Path != nil {
				params["path"] = *fixture.Path
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
