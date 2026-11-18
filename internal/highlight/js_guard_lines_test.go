package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The JavaScript guards decide whether an identifier is an object attribute,
// which depends on the document's line-terminator class: with CR/U+2028/U+2029
// present the reference's ^/$/dot semantics apply. M24 moved that decision from
// a per-call tail rescan to the flag the highlighter already derived, so both
// classes must keep the pinned output.
func TestJavaScriptGuardsRespectDocumentLineClass(t *testing.T) {
	for name, line := range map[string]string{"lf": "\n", "crlf": "\r\n", "u2028": "\u2028"} {
		t.Run(name, func(t *testing.T) {
			src := "const o = {key: 1};" + line
			result, err := Highlight(src, "js")
			if err != nil {
				t.Fatal(err)
			}
			html := RenderHTML(result.Nodes)
			if !strings.Contains(html, `class="hljs-attr">key</span>`) {
				t.Fatalf("object attribute not recognised in %s document: %s", name, html)
			}
		})
	}
}

func BenchmarkJavaScriptObjectAttributes(b *testing.B) {
	src := strings.Repeat("const x = (a) => ({k: \"v\", n: 1}); // comment\n", 1600)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, err := Highlight(src, "js"); err != nil {
			b.Fatal(err)
		}
	}
}

// pi v1.4.0 (4a080252) highlight.js compiles JavaScript regex sources. This
// snapshot covers every compilable compiled pattern against Unicode inputs and
// the Annex B escape, class and quantifier forms where JavaScript differs from
// Go's regexp syntax. Indices are UTF-16 code units.
func TestJavaScriptRegexSources(t *testing.T) {
	data, err := os.ReadFile("testdata/js-regex/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Source string `json:"source"`
		Flags  string `json:"flags"`
		Inputs []struct {
			Input   string `json:"input"`
			Indices []int  `json:"indices"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		re, err := compilePattern(tc.Source, tc.Flags)
		if err != nil {
			t.Errorf("%q: %v", tc.Source, err)
			continue
		}
		for _, in := range tc.Inputs {
			encoded := encodeCodeUnitsString(in.Input)
			if got := codeUnitIndices(encoded, re.FindStringSubmatchIndex(encoded)); !equalInts(got, in.Indices) {
				t.Errorf("%q on %q: indices = %v; want %v", tc.Source, in.Input, got, in.Indices)
			}
		}
	}
}

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

// pi v1.5.0 highlight.js 10.7.3 leaves rejected `0x` tokens plain and still
// highlights the following literal; a run rejected past the dispatch budget is
// left plain as a responsiveness guard, which is D351's intentional difference.
func TestRejectedCandidateRunStopsAtBudget(t *testing.T) {
	short, err := HighlightWith(`0x "s"`, "javascript", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderHTML(short.Nodes); !strings.Contains(got, `<span class="hljs-string">&quot;s&quot;</span>`) {
		t.Fatalf("short run output = %q; want the string highlighted", got)
	}

	long := strings.Repeat("0x", maxRejectedCandidates) + ` "s"`
	result, err := HighlightWith(long, "javascript", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderHTML(result.Nodes); strings.Contains(got, "<span") {
		t.Fatalf("long run output = %q; want plain text after the budget", got)
	}
}
