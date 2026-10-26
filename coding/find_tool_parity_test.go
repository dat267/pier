package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/tools/find.ts with the default fd implementation.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/find-tool/generate.mjs ./pi > coding/testdata/find-tool/golden.json
func TestFindToolMatchesUpstream(t *testing.T) {
	if _, err := execLookPath("fd"); err != nil {
		if _, altErr := execLookPath("fdfind"); altErr != nil {
			t.Skip("fd not installed")
		}
	}
	raw, err := os.ReadFile("testdata/find-tool/golden.json")
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
		t.Fatal("empty find-tool corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dir, "ignored"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"readme.md": "x", "src/a.ts": "x", "src/deep/b.spec.ts": "x",
				"ignored/c.ts": "x", ".gitignore": "ignored/\n", ".hidden.ts": "x",
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
			result, execErr := CreateFindTool(dir).Execute("call", params, context.Background(), nil)
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
			// fd's cross-file traversal order is not stable, so the corpus compares
			// sorted match lines and keeps any trailing notice.
			lines := strings.Split(text.String(), "\n")
			footer := -1
			for index, line := range lines {
				if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
					footer = index
					break
				}
			}
			var body, notices []string
			if footer == -1 {
				body = lines
			} else {
				body, notices = lines[:footer], lines[footer:]
			}
			compacted := make([]string, 0, len(body))
			for _, line := range body {
				if line != "" {
					compacted = append(compacted, line)
				}
			}
			sort.Strings(compacted)
			compacted = append(compacted, notices...)
			if got := strings.Join(compacted, "\n"); got != fixture.Expected.Text {
				t.Fatalf("text differs\n got: %q\nwant: %q", got, fixture.Expected.Text)
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

// fd returns an unspecified subset when a result limit truncates the traversal,
// so only the notice and the count are asserted.
func TestFindToolResultLimitNotice(t *testing.T) {
	if _, err := execLookPath("fd"); err != nil {
		if _, altErr := execLookPath("fdfind"); altErr != nil {
			t.Skip("fd not installed")
		}
	}
	dir := t.TempDir()
	for _, name := range []string{"a.ts", "b.ts", "c.ts", "d.ts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := CreateFindTool(dir).Execute("call", []byte(`{"pattern":"*.ts","limit":2}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := result.Content[0].(ai.TextContent)
	lines := strings.Split(text.Text, "\n")
	if len(lines) < 3 {
		t.Fatalf("output = %q", text.Text)
	}
	if notice := lines[len(lines)-1]; notice != "[2 results limit reached. Use limit=4 for more, or refine pattern]" {
		t.Fatalf("notice = %q", notice)
	}
}
