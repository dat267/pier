package highlight

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252) highlight.js keeps each named sublanguage's top mode
// between segments, so a quote opened before a shell prompt continues inside
// the next prompt's command.
func TestSublanguageContinuations(t *testing.T) {
	data, err := os.ReadFile("testdata/sublanguage-continuations/fixtures.json")
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
