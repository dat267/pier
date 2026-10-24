package tui

import (
	"strings"
	"testing"
)

func TestMarkdownForceItalicSurvivesDetachedPreparation(t *testing.T) {
	var work func() func()
	markdown := NewMarkdown("# Heading\n\n"+strings.Repeat("body ", 14000)+"\n\n`inline`", 0, 0, MarkdownTheme{}, nil, MarkdownOptions{ForceItalic: true})
	markdown.SetPreparation(&MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	markdown.Render(40)
	if work == nil {
		t.Fatal("large Markdown was not prepared off-loop")
	}
	apply := work()
	apply()
	lines := markdown.Render(40)
	for _, line := range lines {
		if line != "" && (!strings.HasPrefix(line, "\x1b[3m") || !strings.HasSuffix(line, "\x1b[23m")) {
			t.Fatalf("prepared line lost italic: %q", line)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "inline") {
		t.Fatal("prepared Markdown is missing inline code")
	}
}

func TestMarkdownForceItalicKeepsImagePayloads(t *testing.T) {
	image := "\x1b_Ga=T,f=100;payload\x1b\\"
	theme := MarkdownTheme{HighlightCode: func(string, string) []string { return []string{image} }}
	markdown := NewMarkdown("```\nignored\n```", 0, 0, theme, nil, MarkdownOptions{ForceItalic: true})
	found := false
	for _, line := range markdown.Render(40) {
		if IsImageLine(line) {
			found = true
			if line != "  "+image {
				t.Fatalf("image payload changed: %q", line)
			}
		}
	}
	if !found {
		t.Fatal("image line missing")
	}
}

// D263 deliberately extends pi v1.4.0 components/markdown.ts: only the
// opt-in thinking presentation overrides resets inside styled elements.
func TestMarkdownForceItalicRestoresResets(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"full", "alpha\x1b[0mbeta", "alpha\x1b[0m\x1b[3mbeta"},
		{"short-full", "alpha\x1b[mbeta", "alpha\x1b[m\x1b[3mbeta"},
		{"selective", "alpha\x1b[23mbeta", "alpha\x1b[23m\x1b[3mbeta"},
		{"combined", "alpha\x1b[22;23;39mbeta", "alpha\x1b[22;23;39m\x1b[3mbeta"},
		{"combined-full", "alpha\x1b[0;31mbeta", "alpha\x1b[0;31m\x1b[3mbeta"},
		{"empty-parameter", "alpha\x1b[;31mbeta", "alpha\x1b[;31m\x1b[3mbeta"},
		{"rgb", "alpha\x1b[38;2;0;23;0mbeta", "alpha\x1b[38;2;0;23;0mbeta"},
		{"palette", "alpha\x1b[38;5;23mbeta", "alpha\x1b[38;5;23mbeta"},
		{"hyperlink", "alpha\x1b]8;;https://example.com/;23m\x07beta\x1b]8;;\x07", "alpha\x1b]8;;https://example.com/;23m\x07beta\x1b]8;;\x07"},
	} {
		t.Run(test.name, func(t *testing.T) {
			theme := MarkdownTheme{HighlightCode: func(string, string) []string { return []string{test.input} }}
			markdown := NewMarkdown("```\nignored\n```", 0, 0, theme, nil, MarkdownOptions{ForceItalic: true})
			lines := markdown.Render(80)
			if !strings.Contains(strings.Join(lines, "\n"), test.want) {
				t.Fatalf("missing inherited italic %q in %q", test.want, lines)
			}
			for _, line := range lines {
				if !strings.HasPrefix(line, "\x1b[3m") || !strings.HasSuffix(line, "\x1b[23m") {
					t.Fatalf("italic not bounded to line: %q", line)
				}
			}
			ordinary := NewMarkdown("```\nignored\n```", 0, 0, theme, nil, MarkdownOptions{})
			if got := strings.Join(ordinary.Render(80), "\n"); !strings.Contains(got, test.input) || strings.Contains(got, "\x1b[3m") {
				t.Fatalf("ordinary Markdown changed: %q", got)
			}
		})
	}
}
