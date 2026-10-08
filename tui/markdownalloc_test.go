package tui

import (
	"runtime"
	"strings"
	"testing"
)

// Marked's token trees (components/markdown.ts) remain independently mutable
// objects. Parse-local allocation batches must preserve that ownership even
// after another document is lexed or garbage collection runs.
func TestMarkdownLexerTextTokensHaveIndependentLifetimes(t *testing.T) {
	for _, paragraphs := range []int{1, 100} {
		name := "small"
		if paragraphs > 1 {
			name = "batched"
		}
		t.Run(name, func(t *testing.T) {
			source := strings.Repeat("Paragraph with **bold**, `code` and a [link](https://example.com) that wraps.\n\n", paragraphs)
			var textTokens []*MdToken
			var collect func([]*MdToken)
			collect = func(tokens []*MdToken) {
				for _, token := range tokens {
					if token.Type == "text" {
						textTokens = append(textTokens, token)
					}
					collect(token.Tokens)
				}
			}
			collect(LexMarkdown(source))
			want := []string{"Paragraph with ", "bold", ", ", " and a ", "link", " that wraps."}
			if len(textTokens) != paragraphs*len(want) {
				t.Fatalf("text token count = %d, want %d", len(textTokens), paragraphs*len(want))
			}
			_ = LexMarkdown(strings.Repeat("Another **document** with a [label](https://example.org).\n\n", 100))
			runtime.GC()
			seen := make(map[*MdToken]bool)
			for i, token := range textTokens {
				if seen[token] {
					t.Fatalf("text token %d aliases an earlier token", i)
				}
				seen[token] = true
				if token.Text != want[i%len(want)] || token.Raw != want[i%len(want)] {
					t.Fatalf("retained text token %d = %+v, want %q", i, token, want[i%len(want)])
				}
			}
			textTokens[0].Text = "changed"
			for i, token := range textTokens[1:] {
				if token.Text != want[(i+1)%len(want)] {
					t.Fatalf("changing one token changed token %d", i+1)
				}
			}
		})
	}
}

// Upstream components/markdown.ts runs the full Marked lexer whenever the
// source changes. Keep that contract while reducing Go-only token allocation
// overhead; this fixture represents a streamed assistant reply with markup.
func TestMarkdownLexerInlineAllocationBudget(t *testing.T) {
	source := strings.Repeat("Paragraph with **bold**, `code` and a [link](https://example.com) that wraps.\n\n", 100)
	var parsed []*MdToken
	allocations := testing.AllocsPerRun(5, func() {
		parsed = LexMarkdown(source)
	})
	paragraphs := 0
	for _, token := range parsed {
		if token.Type == "paragraph" {
			paragraphs++
		}
	}
	if paragraphs != 100 {
		t.Fatalf("paragraphs = %d, want 100", paragraphs)
	}
	// The original lexer allocated about 1,909 objects, including one large
	// MdToken object per plain-text run and growing pointer slices for each
	// marked-up paragraph. Keep headroom without allowing those costs back.
	if allocations > 1150 {
		t.Fatalf("lexing %d bytes allocated %.0f objects, want at most 1150", len(source), allocations)
	}
}
