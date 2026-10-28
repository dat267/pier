package highlight

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252) highlight.js compiles JavaScript regexes: a multiline
// `$` also matches before CR, U+2028 and U+2029. Manual lookahead replacements
// in the REPL and YAML languages must agree.
func TestManualLineTerminatorLookaheads(t *testing.T) {
	data, err := os.ReadFile("testdata/line-terminators/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Name, Language, Code, HTML string }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			result, err := Highlight(tc.Code, tc.Language)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.HTML {
				t.Fatalf("HTML = %q; want %q", got, tc.HTML)
			}
		})
	}
}
