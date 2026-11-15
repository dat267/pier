package markdown

import "testing"

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
