package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
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
			if got := codeUnitIndices(encoded, re.FindStringSubmatchIndex(encoded)); !slices.Equal(got, in.Indices) {
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

// pi v1.6.0 highlight.js 10.7.3 leaves rejected `0x` tokens plain and still
// highlights the following literal, however long the run. Only a rejection
// that needs the regexp counts toward the dispatch budget (D351), so a run the
// walk rejects in O(1) per candidate (a long comment of words that start like
// the comment's phrase rule, or a `0x` run while JavaScript's function rule
// still fits the walk's budget) matches the reference, and only a run whose
// candidates need the regexp is cut off. The expected HTML was read off the
// pinned highlight.js.
func TestRejectedCandidateRunStopsAtBudget(t *testing.T) {
	render := func(code, language string) string {
		t.Helper()
		result, err := HighlightWith(code, language, true)
		if err != nil {
			t.Fatal(err)
		}
		return RenderHTML(result.Nodes)
	}
	run := strings.Repeat("0x", maxRejectedCandidates)
	if got, want := render(run+` "s"`, "javascript"), run+` <span class="hljs-string">&quot;s&quot;</span>`; got != want {
		t.Fatalf("0x run output = %q; want %q", got, want)
	}
	words := strings.Repeat("abc ", 4*maxRejectedCandidates)
	if got, want := render("/* "+words+"*/ int x;", "cpp"),
		`<span class="hljs-comment">/* `+words+`*/</span> <span class="hljs-keyword">int</span> x;`; got != want {
		t.Fatalf("comment output = %q; want %q", got, want)
	}
	tail := ` (<span class="hljs-number">1</span> <span class="hljs-string">&quot;s&quot;</span> <span class="hljs-comment">/* c */</span> <span class="hljs-keyword">int</span>`
	short := strings.Repeat("a", maxRejectedCandidates)
	if got := render(short+` (1 "s" /* c */ int`, "c"); got != short+tail {
		t.Fatalf("C run output = %q; want %q", got, short+tail)
	}

	// A run longer than the walk's budget leaves each candidate to the
	// regexp, which scans the tail, so the cutoff still applies there: the
	// reference renders this C input with the same tail as above, while the
	// port leaves the number, string and comment plain and colours only the
	// keyword, which the appended-text keyword pass still applies (D351).
	long := strings.Repeat("a", 2*exactBudget)
	got := render(long+` (1 "s" /* c */ int`, "c")
	for _, class := range []string{"hljs-number", "hljs-string", "hljs-comment"} {
		if strings.Contains(got, class) {
			t.Fatalf("long C run output = %q; want no %s after the budget", got, class)
		}
	}
	if !strings.Contains(got, `<span class="hljs-keyword">int</span>`) {
		t.Fatalf("long C run output = %q; want the keyword still highlighted", got)
	}
}
