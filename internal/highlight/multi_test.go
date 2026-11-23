package highlight

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPrefixedDispatchKeepsDotInsideOneLine guards the anchored dispatch
// matcher: it consumes the real preceding byte with `(?s:.)`, and that DOTALL
// must not leak into the rules. A leaked `s` flag makes a rule's `.` cross
// newlines, so EBNF's `\?.*\?` special sequence would swallow every later line
// instead of closing on the same line.
func TestPrefixedDispatchKeepsDotInsideOneLine(t *testing.T) {
	result, err := Highlight("a = ? x ? ;\nb = ? y ? ;\n", "ebnf")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	html := RenderHTML(result.Nodes)
	if got := strings.Count(html, `class="hljs-meta"`); got != 2 {
		t.Fatalf("got %d meta spans, want 2; a rule's `.` crossed newlines:\n%s", got, html)
	}
}

// TestIgnoredZeroWidthEndAdvances guards the scan loop's handling of an ignored
// zero-width end. julia-repl ends its sub-language at `^(?![ ]{6})`, ported as a
// zero-width `^` end with an onEnd guard; returning 0 would re-find the same end
// and burn the iteration cap while swallowing the rest of the input.
func TestIgnoredZeroWidthEndAdvances(t *testing.T) {
	result, err := Highlight("julia> foo\n      bar\nbaz\n", "julia-repl")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	html := RenderHTML(result.Nodes)
	if !strings.Contains(html, "</span>baz") {
		t.Fatalf("the unindented line was swallowed by the REPL block:\n%s", html)
	}
}

// TestMidLineBeginLineDoesNotMatch guards the non-dispatch scan path's context
// verification: a `^` rule must not match mid-line just because the scan slice
// starts there. armasm's `^[ \t]*(?=#)` comment rule only matches a line-leading
// `#`, so `mov r0, #1` keeps `#1` as a number.
func TestMidLineBeginLineDoesNotMatch(t *testing.T) {
	result, err := Highlight("mov r0, #1\n", "armasm")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	if html := RenderHTML(result.Nodes); strings.Contains(html, `class="hljs-comment"`) {
		t.Fatalf("a mid-line `#` was read as a line-leading comment:\n%s", html)
	}
	result, err = Highlight("#include <foo.h>\n", "armasm")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	if html := RenderHTML(result.Nodes); !strings.Contains(html, `class="hljs-comment"`) {
		t.Fatalf("a line-leading `#` must still be a comment:\n%s", html)
	}
}

// TestBeginKeywordsPrefersTheLongestMatch guards the beginKeywords compiler:
// JavaScript's `(?=\b|\s)` lookahead backtracks to a longer alternative when
// the boundary after a shorter prefix fails, so `defn` must not be read as
// `def`. RE2's leftmost-first alternation needs the longest word first.
func TestBeginKeywordsPrefersTheLongestMatch(t *testing.T) {
	result, err := Highlight("(defn f []) (def x 1)", "clojure")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	html := RenderHTML(result.Nodes)
	if !strings.Contains(html, "<span class=\"hljs-keyword\">defn</span>") {
		t.Fatalf("`defn` was not read as one keyword:\n%s", html)
	}
	if !strings.Contains(html, "<span class=\"hljs-keyword\">def</span>") {
		t.Fatalf("`def` was not read as a keyword:\n%s", html)
	}
}

// TestNonASCIIRuleStartIsNotSkipped guards the byte pre-filter: it is a set of
// bytes, and strings.IndexAny would decode a non-ASCII leading byte as an
// invalid rune and skip it. 1C's Cyrillic `процедура` rule must still match.
func TestNonASCIIRuleStartIsNotSkipped(t *testing.T) {
	result, err := Highlight("процедура Тест()", "1c")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	if html := RenderHTML(result.Nodes); !strings.Contains(html, `class="hljs-function"`) {
		t.Fatalf("a non-ASCII rule begin was skipped:\n%s", html)
	}
}

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

// A rule set holding the default `\B|\b` end always matches at lastIndex, so
// execAtLastIndex tries the alternation there only. It must pick the same rule
// and lexeme as the unanchored scan (core.js MultiRegex.exec, highlight.js
// 10.7.3 in pi v1.6.0, faac5858) at every position, including after a
// non-ASCII rune and with JavaScript line terminators in the input.
func TestAtLastIndexMatchesTheScan(t *testing.T) {
	inputs := []string{
		"re = /a+/g; return y / 2; // c\n",
		"f(b) => { d }\r\n'h' + `${j}` é 日 \u2028 <a>x</a>",
	}
	checked := 0
	for _, name := range Languages() {
		lang, err := getCompiledLanguage(name)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[*compiledMode]bool{}
		var walk func(mode *compiledMode)
		walk = func(mode *compiledMode) {
			if mode == nil || seen[mode] {
				return
			}
			seen[mode] = true
			if matcher := mode.matcher.getMatcher(0); matcher.atLastIndex {
				for _, input := range inputs {
					jsLines := nonLFLineEnds(input)
					for pos := 0; pos <= len(input); pos++ {
						if pos < len(input) && !utf8.RuneStart(input[pos]) {
							continue // the engine advances by whole runes
						}
						want := matcher.execScan(input, pos, jsLines, nil)
						got := matcher.execAtLastIndex(input, pos, jsLines, nil)
						checked++
						if (want == nil) != (got == nil) || want != nil && (want.index != got.index ||
							want.lexeme != got.lexeme || want.typ != got.typ || want.rule != got.rule) {
							t.Fatalf("%s at %d of %q: anchored %+v, scan %+v", name, pos, input, got, want)
						}
					}
				}
			}
			for _, child := range mode.contains {
				walk(child)
			}
			walk(mode.starts)
		}
		walk(lang.root)
	}
	if checked < 10000 {
		t.Fatalf("only %d positions checked", checked)
	}
}
