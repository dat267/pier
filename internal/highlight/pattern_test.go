package highlight

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// compiledPattern mirrors one regex from highlight.js's compiled language
// definitions, dumped by walking `hljs.getLanguage(name)` after a highlight
// call (see testdata/compiled_patterns.json).
type compiledPattern struct {
	Lang   string `json:"lang"`
	Kind   string `json:"kind"`
	Src    string `json:"src"`
	Flags  string `json:"flags"`
	Groups int    `json:"groups"`
}

func loadCompiledPatterns(t *testing.T) []compiledPattern {
	t.Helper()
	data, err := os.ReadFile("testdata/compiled_patterns.json")
	if err != nil {
		t.Fatalf("read compiled patterns: %v", err)
	}
	var patterns []compiledPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		t.Fatalf("parse compiled patterns: %v", err)
	}
	return patterns
}

// TestPatternsAreRE2Compatible pins the port's central assumption: every
// compiled regex highlight.js builds for pi's eager languages either compiles
// with Go's RE2 (with identical capture-group counts) or is one of the small,
// explicit sets that need a hand-written equivalent.
func TestPatternsAreRE2Compatible(t *testing.T) {
	patterns := loadCompiledPatterns(t)
	if len(patterns) < 4000 {
		t.Fatalf("loaded %d patterns, want the full snapshot", len(patterns))
	}

	var compatible, lookaround, backreference, other int
	for _, p := range patterns {
		if hasLookaround(p.Src) {
			lookaround++
		}
		if hasBackreference(p.Src) {
			backreference++
		}
		if hasLookaround(p.Src) || hasBackreference(p.Src) {
			continue
		}
		re, err := compilePattern(p.Src, p.Flags)
		if err != nil {
			t.Errorf("%s %s %q: %v", p.Lang, p.Kind, p.Src, err)
			other++
			continue
		}
		if got := re.NumSubexp(); got != p.Groups {
			t.Errorf("%s %s %q: %d capture groups, highlight.js has %d", p.Lang, p.Kind, p.Src, got, p.Groups)
			other++
			continue
		}
		compatible++
	}
	// The snapshot is a fixed artifact, so the counts are pinned: a regression
	// in preparePattern shows up as `other`, and a new lookaround shows up as
	// a changed lookaround count.
	if lookaround != 115 {
		t.Errorf("lookaround patterns = %d, want 115", lookaround)
	}
	if backreference != 4 {
		t.Errorf("backreference patterns = %d, want 4", backreference)
	}
	if other != 0 {
		t.Errorf("%d patterns failed to compile with RE2", other)
	}
	if compatible != 4749 {
		t.Errorf("RE2-compatible patterns = %d, want 4749", compatible)
	}
}

func TestNormalizePattern(t *testing.T) {
	cases := []struct{ in, want string }{
		{`\u00A8`, `\x{00a8}`},
		{`[\u00B2-\u00B5]`, `[\x{00b2}-\x{00b5}]`},
		// Without /u, \u{...} is not a code-point escape: it is an identity
		// escape for "u" followed by a brace expression (Annex B).
		{`\u{1F600}`, `u{1F600}`},
		{`\w+`, `\w+`},
		{`\/\*\*`, `\/\*\*`},
	}
	for _, tc := range cases {
		if got := normalizeWhitespaceClasses(normalizeJSEscapes(tc.in)); got != tc.want {
			t.Errorf("escapes and classes of %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// jsSpaceRunes (the pattern fragment substituted into classes) and jsSpaceUnits
// (the rune list the complement rewrite walks) must describe the SAME set. Two
// representations of one ECMAScript property can drift apart silently: a class
// built from the string would then disagree with a complement computed from the
// list, for the same input.
//
// The units are also checked against node's own \s, which is the authority for
// ECMAScript WhiteSpace + LineTerminator; the 25 entries match it exactly.
func TestJavaScriptWhitespaceSetAgreement(t *testing.T) {
	re, err := regexp.Compile("[" + jsSpaceRunes + "]")
	if err != nil {
		t.Fatalf("jsSpaceRunes does not parse as a class: %v", err)
	}
	units := make(map[rune]bool, len(jsSpaceUnits))
	for _, unit := range jsSpaceUnits {
		units[unit] = true
	}
	if len(units) != len(jsSpaceUnits) {
		t.Fatalf("jsSpaceUnits has %d entries but %d distinct", len(jsSpaceUnits), len(units))
	}
	var mismatches []rune
	for r := rune(0); r <= 0x10ffff; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if inClass := re.MatchString(string(r)); inClass != units[r] {
			mismatches = append(mismatches, r)
			if len(mismatches) > 8 {
				break
			}
		}
	}
	if len(mismatches) > 0 {
		t.Fatalf("jsSpaceRunes and jsSpaceUnits disagree at %v", mismatches)
	}
	// The documented 25 code points, from node: \s over every code point.
	want := []rune{
		0x0009, 0x000a, 0x000b, 0x000c, 0x000d, 0x0020, 0x00a0, 0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007,
		0x2008, 0x2009, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff,
	}
	if len(jsSpaceUnits) != len(want) {
		t.Fatalf("jsSpaceUnits has %d entries, want %d", len(jsSpaceUnits), len(want))
	}
	for i := range want {
		if jsSpaceUnits[i] != want[i] {
			t.Errorf("jsSpaceUnits[%d] = U+%04X, want U+%04X", i, jsSpaceUnits[i], want[i])
		}
	}
}

// hasLookaround reports whether src contains a lookahead or lookbehind.
func hasLookaround(src string) bool {
	return strings.Contains(src, "(?=") || strings.Contains(src, "(?!") ||
		strings.Contains(src, "(?<=") || strings.Contains(src, "(?<!")
}

// hasBackreference reports whether src contains a numeric backreference.
func hasBackreference(src string) bool {
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		c := src[i+1]
		if c >= '1' && c <= '9' {
			return true
		}
		i++
	}
	return false
}
