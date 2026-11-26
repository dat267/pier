package markdown

import (
	"strings"
	"testing"
)

func render(t *testing.T, src string, width int) []string {
	t.Helper()
	return RenderSource(src, width, NopStyle{})
}

func TestLayoutParagraphWraps(t *testing.T) {
	got := render(t, "one two three four\n", 8)
	want := []string{"one two", "three", "four"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutParagraphFitsOnOneLine(t *testing.T) {
	got := render(t, "hello world\n", 40)
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestLayoutHeading(t *testing.T) {
	got := render(t, "# Title\n", 40)
	if len(got) != 1 || got[0] != "Title" {
		t.Fatalf("got %q", got)
	}
}

func TestLayoutCodeBlock(t *testing.T) {
	got := render(t, "```go\nfmt.Println(\"hi\")\n```\n", 40)
	want := []string{"```go", "  fmt.Println(\"hi\")", "```"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutThematicBreak(t *testing.T) {
	got := render(t, "---\n", 5)
	if len(got) != 1 || got[0] != strings.Repeat("─", 5) {
		t.Fatalf("got %q", got)
	}
}

func TestLayoutList(t *testing.T) {
	got := render(t, "- one\n- two\n", 20)
	want := []string{"- one", "- two"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutOrderedListNumbers(t *testing.T) {
	got := render(t, "3. three\n4. four\n", 20)
	want := []string{"3. three", "4. four"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutTaskList(t *testing.T) {
	got := render(t, "- [ ] todo\n- [x] done\n", 20)
	want := []string{"- [ ] todo", "- [x] done"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutNestedListIndents(t *testing.T) {
	got := render(t, "- a\n  - b\n", 20)
	want := []string{"- a", "    - b"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutBlockquotePrefixes(t *testing.T) {
	got := render(t, "> quoted\n> more\n", 20)
	want := []string{"│ quoted", "│ more"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutTableColumns(t *testing.T) {
	got := render(t, "| a | b |\n|---|---|\n| 1 | 22 |\n", 40)
	want := []string{
		"┌───┬────┐",
		"│ a │ b  │",
		"├───┼────┤",
		"│ 1 │ 22 │",
		"└───┴────┘",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestLayoutHardBreak(t *testing.T) {
	got := render(t, "a  \nb\n", 40)
	want := []string{"a", "b"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLayoutBlankLineBecomesSpace(t *testing.T) {
	got := render(t, "one\n\ntwo\n", 40)
	want := []string{"one", "", "two"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ansiStyle wraps bold runs in markers so wrapping can be checked against
// escapes.
type ansiStyle struct{ NopStyle }

func (ansiStyle) Bold(text string) string { return "\x1b[1m" + text + "\x1b[0m" }

func TestLayoutWrappingKeepsEscapesBalanced(t *testing.T) {
	got := RenderSource("alpha **beta gamma** delta\n", 12, ansiStyle{})
	for _, line := range got {
		if got := strings.Count(line, "\x1b"); got%2 != 0 {
			t.Fatalf("line %q has unbalanced escapes", line)
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected the line to wrap, got %q", got)
	}
}

func TestVisibleWidthIgnoresEscapes(t *testing.T) {
	if w := visibleWidth("\x1b[1mbold\x1b[0m"); w != 4 {
		t.Fatalf("width = %d, want 4", w)
	}
	if w := visibleWidth("日本語"); w != 6 {
		t.Fatalf("width = %d, want 6", w)
	}
}

// runeWidth must agree with the table the terminal uses (get-east-asian-width via
// x/text/width). A compact hand-written range list disagreed on 8868 East Asian
// Wide/Fullwidth code points, including U+2705 (✅), U+2B50 (⭐) and U+231A (⌚), so a
// line the layout measured as 20 cells occupied 40 and overflowed the row: the wrapped
// rows never broke. Verified against the pinned get-east-asian-width, which answers 2
// for each of these.
func TestRuneWidthMatchesTheTerminalTable(t *testing.T) {
	for _, tc := range []struct {
		r    rune
		want int
	}{
		{0x2705, 2}, // white heavy check mark
		{0x2B50, 2}, // white medium star
		{0x231A, 2}, // watch
		{0x4E00, 2}, // CJK unified ideograph
		{0x1F300, 2},
		{0xFF21, 2}, // fullwidth A
		{0x2318, 1}, // place of interest sign, neutral
		{0x1F650, 1},
		{'a', 1},
		{0x0301, 0}, // combining acute
		{0xFE0F, 0}, // variation selector
	} {
		if got := runeWidth(tc.r); got != tc.want {
			t.Errorf("runeWidth(U+%04X) = %d, want %d", tc.r, got, tc.want)
		}
	}
}

// The consequence: a run of wide emoji must wrap, not stay on one row.
func TestWideEmojiWrapAtTheirRealWidth(t *testing.T) {
	rows := wrapPlain(strings.Repeat("✅", 20), 20)
	if len(rows) < 2 {
		t.Fatalf("20 check marks at 2 cells each stayed on %d row(s) of width 20", len(rows))
	}
	for _, row := range rows {
		cells := 0
		for _, r := range row {
			cells += runeWidth(r)
		}
		if cells > 20 {
			t.Errorf("row %q occupies %d cells, more than 20", row, cells)
		}
	}
}
