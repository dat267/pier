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

// pi v1.4.0 (4a080252), core/tools/grep.ts with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/grep-tool/generate.mjs ./pi > coding/testdata/grep-tool/golden.json
func TestGrepToolMatchesUpstream(t *testing.T) {
	if _, err := os.Stat("/usr/bin/rg"); err != nil {
		if _, lookErr := execLookPath("rg"); lookErr != nil {
			t.Skip("ripgrep not installed")
		}
	}
	raw, err := os.ReadFile("testdata/grep-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string         `json:"name"`
		Args     map[string]any `json:"args"`
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
		t.Fatal("empty grep-tool corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"a.txt":     "alpha one\nbeta two\nalpha three\n",
				"b.txt":     "nothing here\n",
				"sub/c.txt": "deep alpha\n",
				"long.txt":  strings.Repeat("x", 600) + "NEEDLE\n",
				"many.txt":  manyGrepLines(12),
			}
			if fixture.Name == "literal" {
				files["c.txt"] = "a.b\naxb\n"
			}
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			params, err := json.Marshal(fixture.Args)
			if err != nil {
				t.Fatal(err)
			}
			result, execErr := CreateGrepTool(dir).Execute("call", params, context.Background(), nil)
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

func manyGrepLines(count int) string {
	var builder strings.Builder
	for index := 1; index <= count; index++ {
		builder.WriteString("match ")
		builder.WriteString(itoaForTest(index))
		builder.WriteString("\n")
	}
	return builder.String()
}
