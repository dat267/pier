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

func readToolText(t *testing.T, dir string, params string) (string, error) {
	t.Helper()
	result, err := CreateReadTool(dir, nil).Execute("id", json.RawMessage(params), context.Background(), nil)
	if err != nil {
		return "", err
	}
	return result.Content[0].(ai.TextContent).Text, nil
}

// read.ts checks `userLimitedLines !== undefined`, so a negative limit (the
// schema is Type.Number) still gets the remaining-lines notice, counted from
// startLine + limit; the expected texts were produced by the upstream text
// path (pi v1.5.0 packages/coding-agent/src/core/tools/read.ts).
func TestReadToolNegativeLimitStillReportsRemainingLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ params, want string }{
		{`{"path":"f.txt","offset":1,"limit":-1}`, "a\nb\n\n[4 more lines in file. Use offset=0 to continue.]"},
		{`{"path":"f.txt","offset":3,"limit":-3}`, "\n\n[4 more lines in file. Use offset=0 to continue.]"},
		{`{"path":"f.txt","limit":0}`, "\n\n[3 more lines in file. Use offset=1 to continue.]"},
	} {
		got, err := readToolText(t, dir, tc.params)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.params, got, err, tc.want)
		}
	}
}

// D400: read.ts indexes allLines[startLine] with a fractional startLine for
// the oversized-first-line notice, so Buffer.byteLength(undefined) throws a
// TypeError. Pier reports the line's size from the slice it actually read.
func TestReadToolFractionalOffsetReportsOversizedLine(t *testing.T) {
	dir := t.TempDir()
	content := "a\n" + strings.Repeat("y", 52000) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readToolText(t, dir, `{"path":"f.txt","offset":2.5}`)
	want := "[Line 2.5 is 50.8KB, exceeds 50.0KB limit. Use bash: sed -n '2.5p' f.txt | head -c 51200]"
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}
