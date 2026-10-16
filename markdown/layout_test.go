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
