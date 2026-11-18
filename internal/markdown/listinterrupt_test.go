package markdown

import (
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. Its paragraph rule may
// only be interrupted by an ordered marker whose number is 1:
//
//	ze = le(/ {0,3}(?:[*+-]|1[.)])[ \t]+[^ \t\n]/)   // paragraph start
//
// so `text\n1. x` is a paragraph plus a list, while `text\n2. x` stays one
// paragraph. Any bullet still interrupts.
func TestParagraphInterruptionByListMarkers(t *testing.T) {
	for _, testCase := range []struct {
		src   string
		want  string
		block []BlockKind
	}{
		{"text\n2. x\n", `p:"text\n2. x"`, []BlockKind{BlockParagraph}},
		{"text\n0. x\n", `p:"text\n0. x"`, []BlockKind{BlockParagraph}},
		{"text\n5) x\n", `p:"text\n5) x"`, []BlockKind{BlockParagraph}},
		{"text\n2) x\n", `p:"text\n2) x"`, []BlockKind{BlockParagraph}},
		{"text\n10. x\n", `p:"text\n10. x"`, []BlockKind{BlockParagraph}},
		{"text\n1. x\n", `p:"text" | ol[(p:"x")]`, []BlockKind{BlockParagraph, BlockList}},
		{"text\n1) x\n", `p:"text" | ol[(p:"x")]`, []BlockKind{BlockParagraph, BlockList}},
		{"text\n- x\n", `p:"text" | ul[(p:"x")]`, []BlockKind{BlockParagraph, BlockList}},
		{"text\n* x\n", `p:"text" | ul[(p:"x")]`, []BlockKind{BlockParagraph, BlockList}},
		{"text\n+ x\n", `p:"text" | ul[(p:"x")]`, []BlockKind{BlockParagraph, BlockList}},
	} {
		blocks := Lex(testCase.src)
		if got := referenceShape(blocks); got != testCase.want {
			t.Errorf("%q: got %s, want %s", testCase.src, got, testCase.want)
		}
		if got := blockKinds(blocks); len(got) != len(testCase.block) {
			t.Fatalf("%q: blocks %v, want %v", testCase.src, got, testCase.block)
		}
	}
}

// TestOrderedMarkerAfterAContainerStartsANewList: any marker ends a container
// item (the reference breaks on nextBulletRegex, which accepts every marker), so
// both variants produce a second list. Inside a paragraph the 1-only rule still
// decides whether the marker becomes a list at all.
func TestOrderedMarkerAfterAContainerStartsANewList(t *testing.T) {
	for _, src := range []string{"- a\ntext\n2. x\n", "- a\ntext\n1. x\n"} {
		blocks := Lex(src)
		if len(blocks) != 2 || blocks[0].Kind != BlockList || blocks[1].Kind != BlockList {
			t.Fatalf("%q: blocks = %v, want two lists", src, blockKinds(blocks))
		}
		if got, want := blocks[0].Blocks[0].Text, "a\ntext"; got != want {
			t.Errorf("%q: item text = %q, want %q", src, got, want)
		}
	}
}

// TestListInterruptionNeedsContentAfterTheMarker: the reference's pattern
// requires a non-space character after the marker, so a bare marker does not
// interrupt a paragraph.
func TestListInterruptionNeedsContentAfterTheMarker(t *testing.T) {
	for _, src := range []string{"text\n1.\n", "text\n* \n", "text\n1. \n"} {
		blocks := Lex(src)
		if len(blocks) != 1 || blocks[0].Kind != BlockParagraph {
			t.Errorf("%q: blocks = %v, want one paragraph", src, blockKinds(blocks))
		}
	}
}

// TestListMarkerInterruptHandlesShortLines pins the bounds: listItemStart
// reports a content indent as a visual column, so a marker at the end of a line
// (or followed by tabs) must be bounded by the marker's own end, not indexed
// with that column. An unbounded slice panicked the incremental lexer.
func TestListMarkerInterruptHandlesShortLines(t *testing.T) {
	for _, line := range []string{"1.", "1. ", "-", "- ", "1.\t", "12.", "1.\t\t", "  1."} {
		if listMarkerInterrupts(line) {
			t.Errorf("%q interrupts a paragraph, want no content", line)
		}
	}
	for _, line := range []string{"1. x", "- x", "1.\tx"} {
		if !listMarkerInterrupts(line) {
			t.Errorf("%q does not interrupt a paragraph", line)
		}
	}
}

// TestBareMarkerDoesNotInterrupt keeps the marker/heading ambiguity explicit:
// `text\n-` is a setext heading (level 2) in the reference, not a list, and
// `text\n1.` stays a paragraph.
func TestBareMarkerDoesNotInterrupt(t *testing.T) {
	if blocks := Lex("text\n-\n"); len(blocks) != 1 || blocks[0].Kind != BlockHeading || blocks[0].Level != 2 {
		t.Fatalf("blocks = %v, want one level-2 heading", blockKinds(blocks))
	}
	for _, src := range []string{"text\n1.\n", "text\n* \n", "text\n1. \n"} {
		if blocks := Lex(src); len(blocks) != 1 || blocks[0].Kind != BlockParagraph {
			t.Errorf("%q: blocks = %v, want one paragraph", src, blockKinds(blocks))
		}
	}
}

// Reference: pi v1.5.2 (b4afdddf) marked 18.0.11 Tokenizer.list, which upstream
// packages/tui/src/components/markdown.ts lexes with. Its loose rule has two
// parts: an item whose raw ends with `/\n[ \t]*\n[ \t]*$/` makes the list loose
// once a later item is pushed, and an item whose space token matches
// `/\n.*\n/` makes it loose at once. A blank line a nested list swallows is not
// in the outer item's token stream, so the outer list stays tight. Pier read
// any blank line inside the list as loose and rendered the extra separator.
func TestListTightnessFollowsMarkedRules(t *testing.T) {
	for _, testCase := range []struct {
		src   string
		tight bool
		nest  bool // the nested list's own tightness, when it has one
	}{
		{"- a\n- b\n", true, false},
		{"- a\n\n- b\n", false, false},
		{"- a\n\n  b\n- c\n", false, false},
		{"- a\n  b\n\n- c\n", false, false},
		{"- [ ] a\n\n- [x] b\n", false, false},
		// The blank line belongs to the nested list, which is loose.
		{"- a\n  - b\n\n    c\n- d\n", true, false},
		// Here it is in the outer item's own token stream, before the list.
		{"- a\n\n  - b\n- c\n", false, true},
		{"- a\n  - b\n\n  - c\n- d\n", true, false},
		// A blank line after the last item has no later item to loosen, so the
		// list stays tight (marked pushes the item before applying the rule).
		{"- a\n- b\n\nparagraph\n", true, false},
		// The blank line may carry spaces or tabs and still count.
		{"- a\n \n- b\n", false, false},
		{"- a\n\t\n- b\n", false, false},
		// A tab-indented continuation belongs to the item.
		{"- a\n\tb\n", true, false},
	} {
		blocks := Lex(testCase.src)
		if len(blocks) == 0 || blocks[0].Kind != BlockList {
			t.Fatalf("%q: blocks = %v, want a list", testCase.src, blockKinds(blocks))
		}
		list := blocks[0]
		if list.Tight != testCase.tight {
			t.Errorf("%q: tight = %v, want %v", testCase.src, list.Tight, testCase.tight)
		}
		if testCase.nest {
			nested := false
			for _, item := range list.Blocks {
				for _, child := range item.Blocks {
					if child.Kind == BlockList {
						nested = true
						if !child.Tight {
							t.Errorf("%q: nested list tight = false, want true", testCase.src)
						}
					}
				}
			}
			if !nested {
				t.Errorf("%q: no nested list found", testCase.src)
			}
		}
	}
}
