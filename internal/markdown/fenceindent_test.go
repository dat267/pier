package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. Its fence tokenizer
// applies indentCodeCompensation:
//
//	const matchIndentToCode = raw.match(/^(\s+)(?:```)/);
//	... line leading whitespace length >= indentToCode.length
//	      ? node.slice(indentToCode.length) : node
//
// so a backtick fence opened after 1-3 spaces removes that many leading
// whitespace characters from every body line that has at least as many. Tilde
// fences are not compensated (the regex only matches backticks).
func TestIndentedFenceStripsItsIndentFromTheBody(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		want string
	}{
		{"  ```\n  foo\n  ```\n", "foo"},
		{"   ```\n   foo\n   ```\n", "foo"},
		{" ```js\n  foo\n   bar\n ```\n", " foo\n  bar"},
		// Lines with less indentation than the fence keep what they have.
		{"  ```\n foo\n  bar\n   baz\n  ```\n", " foo\nbar\n baz"},
		// JS \s counts, so a non-breaking space before content is whitespace.
		{"  ```\n\u00a0foo\n  bar\n  ```\n", "\u00a0foo\nbar"},
		// Marked's regex only recognizes a backtick fence here.
		{"  ~~~\n  foo\n  ~~~\n", "  foo"},
		// Unclosed and empty bodies go through the same compensation.
		{"   ```\n   ", ""},
		{"  ```\n  foo\n  ``", "foo\n``"},
	} {
		blocks := Lex(testCase.src)
		TrimPartialClosingFences(blocks)
		if len(blocks) != 1 || blocks[0].Kind != BlockCode {
			t.Fatalf("%q: blocks = %v, want one code block", testCase.src, blockKinds(blocks))
		}
		if got := blocks[0].Body; got != testCase.want {
			t.Errorf("%q: body = %q, want %q", testCase.src, got, testCase.want)
		}
	}
}

// TestIndentedFenceInsideAListItemIsAlreadyDedented: the list lexer hands the
// item's content to the block lexer without the item indent, so the fence
// starts at column 0 and compensation does nothing. Pinned because the
// compensation must not strip real content spaces from a nested block.
func TestIndentedFenceInsideAListItemIsAlreadyDedented(t *testing.T) {
	blocks := Lex("- item\n\n  ```\n  foo\n  ```\n")
	item := blocks[0]
	if item.Kind != BlockList || len(item.Blocks) == 0 {
		t.Fatalf("blocks = %v, want a list", blockKinds(blocks))
	}
	var got string
	for _, content := range item.Blocks[len(item.Blocks)-1].Blocks {
		if content.Kind == BlockCode {
			got = content.Body
		}
	}
	if got != "foo" {
		t.Fatalf("nested body = %q, want %q", got, "foo")
	}
}
