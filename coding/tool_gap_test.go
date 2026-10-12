package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream read.ts:136 uses `textContent.split("\n")`, which keeps the empty
// element after a trailing newline. The port used a helper that dropped it, so
// the read output lost the final newline and the line count was one low.
func TestReadToolKeepsTrailingNewlineAndLineCount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "s.txt"), []byte("a\nb\n"), 0o644)
	tool := CreateReadTool(dir, nil)

	text := execTool(t, tool, `{"path":"s.txt"}`).Content[0].(ai.TextContent).Text
	if text != "a\nb\n" {
		t.Fatalf("content = %q, want %q", text, "a\nb\n")
	}
	// The empty line after the trailing newline counts, like JS split("\n").
	text = execTool(t, tool, `{"path":"s.txt","offset":3}`).Content[0].(ai.TextContent).Text
	if text != "" {
		t.Fatalf("offset 3 = %q, want the empty trailing line", text)
	}
}

// Upstream generateDiffString sizes the line-number column from
// `oldContent.split("\n").length`, which counts the empty element after a
// trailing newline. That changes the column width at a power-of-ten boundary.
func TestGenerateDiffStringLineNumberWidthCountsTrailingNewline(t *testing.T) {
	oldContent := "a\n" + strings.Repeat("x\n", 8)
	newContent := "b\n" + strings.Repeat("x\n", 8)
	diff, _, _ := GenerateDiffString(oldContent, newContent, 4)
	if !strings.Contains(diff, "- 1 a") || !strings.Contains(diff, "+ 1 b") {
		t.Fatalf("diff = %q, want two-wide line numbers", diff)
	}
}

// Upstream isSingleEditInput accepts any two string fields, including an empty
// oldText; the string-form branch required a non-empty one.
func TestPrepareEditArgumentsAcceptsEmptyOldTextInStringForm(t *testing.T) {
	got := PrepareEditArguments(json.RawMessage(`{"path":"p","edits":"{\"oldText\":\"\",\"newText\":\"b\"}"}`))
	var args struct {
		Edits []json.RawMessage `json:"edits"`
	}
	if err := json.Unmarshal(got, &args); err != nil {
		t.Fatalf("edits were not normalized to an array: %s (%v)", got, err)
	}
	if len(args.Edits) != 1 {
		t.Fatalf("edits = %s, want one normalized edit", got)
	}
}
