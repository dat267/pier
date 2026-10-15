package interactive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// TestFormatReadLineRange ports the raw-argument cases of upstream
// read-line-range-display.test.ts (#9887/#10938): finite numbers and numeric
// strings format a `:start-end` label, omitted optionals default the start to
// 1, and a malformed range is omitted.
func TestFormatReadLineRange(t *testing.T) {
	cases := []struct {
		offset any
		limit  any
		want   string
	}{
		{25, 13, ":25-37"},
		{"25", "13", ":25-37"},
		{nil, "13", ":1-13"},
		{"25", nil, ":25"},
		{nil, nil, ""},
		{"abc", 13, ""},
		{25, "", ""},
		{25, "Infinity", ""},
	}
	for _, testCase := range cases {
		if got := formatReadLineRange(testCase.offset, testCase.limit); got != testCase.want {
			t.Errorf("formatReadLineRange(%v, %v) = %q, want %q", testCase.offset, testCase.limit, got, testCase.want)
		}
	}
}

// The TUI, the session tree and the HTML export share the formatter, so a
// string offset/limit renders the same range in each (#10938).
func TestReadLineRangeDisplayPaths(t *testing.T) {
	InitTheme("dark", false)
	args := map[string]any{"path": "notes.txt", "offset": "25", "limit": "13"}
	cwd := t.TempDir()

	// TUI read tool header.
	call := tui.StripTerminalSequences(formatReadCall(args, CurrentTheme(), cwd))
	if !strings.Contains(call, "read notes.txt:25-37") {
		t.Errorf("read call = %q", call)
	}

	// Session tree row.
	if row := (&TreeList{}).formatToolCall("read", args); row != "[read: notes.txt:25-37]" {
		t.Errorf("tree row = %q", row)
	}

	// HTML export viewer: the shared function is embedded and used by the read
	// case (the viewer renders at view time, so the Go test pins the source).
	source, err := os.ReadFile(filepath.Join("exporthtml", "template.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "formatReadLineRange(args.offset, args.limit)") {
		t.Error("exporthtml/template.js read case does not use the shared range formatter")
	}
}
