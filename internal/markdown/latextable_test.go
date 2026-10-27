package markdown

import (
	"slices"
	"strings"
	"testing"
)

// Upstream v1.4.0 2ed3118b0 (#11159): an unclosed `$$` or `\[` must not pair
// with a later opener across a GFM table, and a pending streaming block must not
// treat a following table as math, because either one hides the table.

// TestContainsTableStart pins the table-start rule: a header row followed by a
// delimiter row, where a pipe-free header needs a pipe in the delimiter and a
// pipe-free delimiter needs one in the header (otherwise it is a thematic
// break).
func TestContainsTableStart(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"pipes both rows", []string{"| A | B |", "|---|---|"}, true},
		{"pipe-free delimiter", []string{"| A |", ":---:"}, true},
		{"pipe-free header", []string{"Value", "| --- |"}, true},
		{"neither row has a pipe", []string{"Value", "---"}, false},
		{"header without a delimiter", []string{"| A | B |", "| 1 | 2 |"}, false},
		{"delimiter without a header", []string{"|---|---|"}, false},
		{"later pair", []string{"x = 1", "", "| A |", "|---|", "| 1 |"}, true},
	}
	for _, tc := range cases {
		if got := containsTableStart(tc.lines); got != tc.want {
			t.Fatalf("%s: containsTableStart(%q) = %v", tc.name, tc.lines, got)
		}
	}
}

// TestLexLatexStopsAtTableStart: the opener becomes ordinary text and the table
// lexes as a table.
func TestLexLatexStopsAtTableStart(t *testing.T) {
	for _, opener := range []string{"$$", `\[`} {
		src := opener + "\nx = 1\n\n| A | B |\n|---|---|\n| 1 | 2 |\n"
		blocks := Lex(src)
		var kinds []BlockKind
		for _, block := range blocks {
			kinds = append(kinds, block.Kind)
		}
		if containsKind(kinds, BlockLatex) {
			t.Fatalf("%s: block math swallowed the table: %v", opener, kinds)
		}
		if !containsKind(kinds, BlockTable) {
			t.Fatalf("%s: no table block: %v", opener, kinds)
		}
	}
	// A closed block still lexes as math, and a table after it still renders.
	blocks := Lex("$$\na\n+ b\n$$\n\n| A |\n|---|\n| 1 |\n")
	if len(blocks) == 0 || blocks[0].Kind != BlockLatex || blocks[0].Pending {
		t.Fatalf("closed math = %+v", blocks)
	}
}

// TestRendererUnclosedMathBeforeTable mirrors the upstream rendering cases.
func TestRendererUnclosedMathBeforeTable(t *testing.T) {
	table := []string{"| A | B |", "|---|---|", "| 1 | 2 |"}
	border := "┌───┬───┐"
	opts := Options{LatexEnabled: true, Latex: func(source string, display bool) (string, bool) {
		if !display {
			return "", false
		}
		return "MATH(" + source + ")", true
	}}
	render := func(source string) []string {
		var lines []string
		for _, line := range renderBlocksTest(t, source, 80, opts) {
			lines = append(lines, strings.TrimRight(line, " "))
		}
		return lines
	}

	// An orphan opener before a table leaves the table visible.
	for _, source := range []string{
		strings.Join([]string{"Intro", "", "$$", ""}, "\n") + "\n" + strings.Join(table, "\n") + "\n\nAfter",
		strings.Join([]string{"$$", "x = 1", ""}, "\n") + "\n" + strings.Join(table, "\n"),
		`\[` + "\nx^2\n\n" + strings.Join(table, "\n"),
	} {
		got := render(source)
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, border) {
			t.Fatalf("table missing from:\n%s", joined)
		}
		if strings.Contains(joined, "MATH(") {
			t.Fatalf("orphan math rendered instead of the table:\n%s", joined)
		}
	}

	// A later closed block still renders, so the opener did not pair across the
	// table.
	got := render(strings.Join([]string{"$$", "x = 1", "", "| A | B |", "|---|---|", "| 1 | 2 |", "", "$$", "y^2", "$$"}, "\n"))
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, border) || !strings.Contains(joined, "MATH(y^2)") {
		t.Fatalf("math paired across the table:\n%s", joined)
	}
}

func containsKind(kinds []BlockKind, want BlockKind) bool {
	return slices.Contains(kinds, want)
}
