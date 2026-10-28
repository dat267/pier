package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// pi v1.4.0 (4a080252), highlight.js core.js langRe compiles JavaScript
// regexes. The Node fixture covers ECMAScript WhiteSpace + LineTerminator,
// including the NEL/zero-width controls that are not whitespace there.
func TestJavaScriptWhitespaceClasses(t *testing.T) {
	data, err := os.ReadFile("testdata/js-classes/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Regex []struct {
			Name, Source, Input string
			Indices             []int
		}
		Highlights []struct{ Name, Language, Code, HTML string }
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Regex {
		t.Run("regex/"+tc.Name, func(t *testing.T) {
			re, err := compilePattern(tc.Source, "m")
			if err != nil {
				t.Fatal(err)
			}
			if got := re.FindStringSubmatchIndex(tc.Input); !reflect.DeepEqual(got, tc.Indices) {
				t.Fatalf("%q on %q = %v; want %v", tc.Source, tc.Input, got, tc.Indices)
			}
		})
	}
	for _, tc := range fixture.Highlights {
		t.Run("highlight/"+tc.Name, func(t *testing.T) {
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
