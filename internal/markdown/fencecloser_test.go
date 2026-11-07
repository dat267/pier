package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11's closing-fence pattern,
// which upstream packages/tui/src/components/markdown.ts lexes with:
//
//	(?: {0,3}\1[~`]* *(?=\n|$)|$)
//
// After the opener (`\1`), any run of backticks and tildes is allowed, then
// spaces only. So "```~~~" closes a "```" fence and renders what follows as a
// paragraph, "~~~```" closes a "~~~" fence, and a tab after the run does not
// close it.
func TestClosingFenceAcceptsAMixedFenceCharacterRun(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		src   string
		want  []BlockKind
		after string
	}{
		{
			name:  "backtick opener, mixed closer",
			src:   "```\na\n```~~~\nb\n",
			want:  []BlockKind{BlockCode, BlockParagraph},
			after: "b",
		},
		{
			name:  "tilde opener, mixed closer",
			src:   "~~~\na\n~~~```\nb\n",
			want:  []BlockKind{BlockCode, BlockParagraph},
			after: "b",
		},
		{
			name:  "mixed closer with trailing spaces",
			src:   "```\na\n```~~~  \nb\n",
			want:  []BlockKind{BlockCode, BlockParagraph},
			after: "b",
		},
		{
			name:  "longer run still closes",
			src:   "```\na\n`````\nb\n",
			want:  []BlockKind{BlockCode, BlockParagraph},
			after: "b",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			blocks := Lex(testCase.src)
			got := blockKinds(blocks)
			if len(got) != len(testCase.want) {
				t.Fatalf("blocks = %v, want %v", got, testCase.want)
			}
			for index := range got {
				if got[index] != testCase.want[index] {
					t.Fatalf("blocks = %v, want %v", got, testCase.want)
				}
			}
			if blocks[0].Body != "a" {
				t.Errorf("code body = %q, want %q", blocks[0].Body, "a")
			}
			if text := blocks[1].Inlines[0].Text; text != testCase.after {
				t.Errorf("following text = %q, want %q", text, testCase.after)
			}
		})
	}
}

// TestTrailingTabDoesNotCloseAFence pins the other half of the same pattern:
// the closing line's only trailing whitespace is spaces, so a tab keeps the
// fence open and the line inside the code block.
func TestTrailingTabDoesNotCloseAFence(t *testing.T) {
	blocks := Lex("```\na\n```\t\nb\n")
	if len(blocks) != 1 || blocks[0].Kind != BlockCode {
		t.Fatalf("blocks = %v, want one code block", blockKinds(blocks))
	}
	if want := "a\n```\t\nb"; blocks[0].Body != want {
		t.Fatalf("body = %q, want %q", blocks[0].Body, want)
	}
}

// TestOtherFenceCharactersDoNotCloseAFence keeps the scoping honest: only a run
// that starts with the opener's own characters closes it.
func TestOtherFenceCharactersDoNotCloseAFence(t *testing.T) {
	blocks := Lex("```\na\n~~~\nb\n")
	if len(blocks) != 1 || blocks[0].Kind != BlockCode {
		t.Fatalf("blocks = %v, want one code block", blockKinds(blocks))
	}
	if want := "a\n~~~\nb"; blocks[0].Body != want {
		t.Fatalf("body = %q, want %q", blocks[0].Body, want)
	}
}
