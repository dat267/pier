package highlight

import (
	"encoding/json"
	"os"
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
	// in normalizePattern shows up as `other`, and a new lookaround shows up as
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
		{`\u00A8`, `\x{00A8}`},
		{`[\u00B2-\u00B5]`, `[\x{00B2}-\x{00B5}]`},
		{`\u{1F600}`, `\x{1F600}`},
		{`\w+`, `\w+`},
		{`\/\*\*`, `\/\*\*`},
	}
	for _, tc := range cases {
		if got := normalizePattern(tc.in); got != tc.want {
			t.Errorf("normalizePattern(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
