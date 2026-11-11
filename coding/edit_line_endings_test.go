package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runEdit(t *testing.T, dir, path, oldText, newText string) {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"path":  path,
		"edits": []map[string]any{{"oldText": oldText, "newText": newText}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateEditTool(dir).Execute("c", params, nil, nil); err != nil {
		t.Fatalf("edit %s: %v", path, err)
	}
	_ = os.Getenv
}

// pi v1.5.2 (b4afdddf) core/tools/edit-diff.ts applyReplacementsToOriginal: text
// outside the replaced ranges keeps its bytes, including its line endings, and
// line breaks in the new text take the ending of the line the range starts on.
// The v1.5.0 route detected one dominant ending for the whole file and rewrote
// every line with it, so editing one line of a mixed-ending file converted the
// rest, and a lone-CR file came back as LF.
func TestEditKeepsUnchangedLineEndings(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		oldText  string
		newText  string
		expected string
	}{
		{
			name:     "mixed-crlf-then-lf",
			content:  "keep-crlf\r\nsecond\nthird\r\n",
			oldText:  "second",
			newText:  "second-edited",
			expected: "keep-crlf\r\nsecond-edited\nthird\r\n",
		},
		{
			name:     "crlf-file-multiline-insert",
			content:  "one\r\ntwo\r\nthree\r\n",
			oldText:  "two",
			newText:  "2a\n2b",
			expected: "one\r\n2a\r\n2b\r\nthree\r\n",
		},
		{
			name:     "lf-file-multiline-insert",
			content:  "one\ntwo\nthree\n",
			oldText:  "two",
			newText:  "2a\n2b",
			expected: "one\n2a\n2b\nthree\n",
		},
		{
			name:     "lone-cr-file",
			content:  "a\rb\rc",
			oldText:  "b",
			newText:  "b2",
			expected: "a\rb2\rc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f.txt")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			runEdit(t, dir, "f.txt", tc.oldText, tc.newText)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Fatalf("file = %q; want %q", got, tc.expected)
			}
		})
	}
}
