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

// Upstream truncate.ts serializes camelCase keys and `truncatedBy: null` when
// no limit fired. The port's struct had no tags and used "" for none.
func TestTruncationResultJSONMatchesPi(t *testing.T) {
	notTruncated := TruncationResult{
		Content: "a\nb\n", TotalLines: 2, TotalBytes: 4,
		OutputLines: 2, OutputBytes: 4, MaxLines: 2000, MaxBytes: 51200,
	}
	enc, err := ai.MarshalJSON(notTruncated)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"content":"a\nb\n","truncated":false,"truncatedBy":null,"totalLines":2,"totalBytes":4,"outputLines":2,"outputBytes":4,"lastLinePartial":false,"firstLineExceedsLimit":false,"maxLines":2000,"maxBytes":51200}`
	if string(enc) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", enc, want)
	}

	truncated := TruncationResult{
		Content: "a", Truncated: true, TruncatedBy: TruncatedByLines,
		TotalLines: 3, TotalBytes: 9, OutputLines: 1, OutputBytes: 1,
		MaxLines: 2000, MaxBytes: 51200,
	}
	enc, err = ai.MarshalJSON(truncated)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"content":"a","truncated":true,"truncatedBy":"lines","totalLines":3,"totalBytes":9,"outputLines":1,"outputBytes":1,"lastLinePartial":false,"firstLineExceedsLimit":false,"maxLines":2000,"maxBytes":51200}`
	if string(enc) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", enc, want)
	}
}

// Upstream read.ts sets details only in its two truncation branches; the port
// persisted a truncation object (with the full file text) on every read.
func TestReadToolOmitsDetailsWhenNotTruncated(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "s.txt"), []byte("a\nb\n"), 0o644)
	tool := CreateReadTool(dir, nil)
	if got := execTool(t, tool, `{"path":"s.txt"}`).Details; len(got) != 0 {
		t.Fatalf("details = %s, want omitted", got)
	}
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("line\n", 3000)), 0o644)
	got := execTool(t, tool, `{"path":"big.txt"}`).Details
	if !strings.Contains(string(got), `"truncated":true`) {
		t.Fatalf("details = %s, want a truncation", got)
	}
	// The image branch also leaves details undefined upstream.
	os.WriteFile(filepath.Join(dir, "img.png"), solidPNG(t, 8, 8), 0o644)
	if got := execTool(t, tool, `{"path":"img.png"}`).Details; len(got) != 0 {
		t.Fatalf("image details = %s, want omitted", got)
	}
}

// Upstream write.ts returns details: undefined.
func TestWriteToolOmitsDetails(t *testing.T) {
	dir := t.TempDir()
	tool := CreateWriteTool(dir)
	if got := execTool(t, tool, `{"path":"n.txt","content":"hi"}`).Details; len(got) != 0 {
		t.Fatalf("details = %s, want omitted", got)
	}
}

// Upstream ls.ts returns details only when a notice fired.
func TestLsToolOmitsDetailsWithoutNotices(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	tool := CreateLsTool(dir)
	if got := execTool(t, tool, `{}`).Details; len(got) != 0 {
		t.Fatalf("details = %s, want omitted", got)
	}
}

// Upstream bash.ts returns details only when the output was truncated.
func TestBashToolOmitsDetailsWithoutTruncation(t *testing.T) {
	dir := t.TempDir()
	tool := CreateBashTool(dir, nil)
	result, err := tool.Execute("c", json.RawMessage(`{"command":"echo hello"}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Details) != 0 {
		t.Fatalf("details = %s, want omitted", result.Details)
	}
}

// Upstream find.ts returns details only when a notice fired.
func TestFindToolOmitsDetailsWithoutNotices(t *testing.T) {
	requireBinary(t, "fd")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	tool := CreateFindTool(dir)
	if got := execTool(t, tool, `{"pattern":"*.txt"}`).Details; len(got) != 0 {
		t.Fatalf("details = %s, want omitted", got)
	}
}

// Upstream grep.ts returns details only when a notice fired.
func TestGrepToolOmitsDetailsWithoutNotices(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle\n"), 0o644)
	tool := CreateGrepTool(dir)
	if got := execTool(t, tool, `{"pattern":"needle"}`).Details; len(got) != 0 {
		t.Fatalf("details = %s, want omitted", got)
	}
}
