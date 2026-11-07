package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) packages/tui/src/components/markdown.ts
// trimPartialClosingFences keeps a streamed, still-unclosed code block's body
// whole and removes only the cut-off closing fence:
//
//	const marker = /^(`{3,}|~{3,})/.exec(token.raw)?.[1];
//	const lastLine = token.raw.split("\n").pop();
//	... token.text = token.text.slice(0, -lastLine.length).replace(/\n$/, "");
//
// Marked keeps the newline-less last line in token.text (`"~~~\nhello\n~~"`
// tokenizes to text `"hello\n~~"`), so exactly one partial line is removed.
func TestUnclosedFenceKeepsItsLastLine(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		want string
	}{
		{"~~~\nhello\n~~", "hello"}, // partial closing fence, once
		{"~~~\nhello\n~", "hello"},
		{"```\nhello\n``", "hello"},
		{"```\nhello\n`", "hello"},
		{"~~~\nhello", "hello"},      // partial content line, not a fence
		{"```\nhello", "hello"},      // marked token.text is "hello"
		{"~~~\nhello\n~~~", "hello"}, // closed fence keeps its content
		{"~~~\n~~~", ""},             // empty block, partial closing fence
		{"~~~\n~", ""},               // empty block, one fence character
		{"~~~\n~~", ""},
		{"```\n`", ""},
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

// TestTrimPartialClosingFencesClampsLikeSlice pins the JS slice semantics the
// reference relies on: `text.slice(0, -n)` returns "" when the body is shorter
// than n instead of failing. Pier sliced unclamped, which panicked inside
// Markdown.Render and killed the frame.
func TestTrimPartialClosingFencesClampsLikeSlice(t *testing.T) {
	block := &Block{Kind: BlockCode, Raw: "~~~\n~~"}
	TrimPartialClosingFences([]*Block{block})
	if block.Body != "" {
		t.Fatalf("body = %q, want %q", block.Body, "")
	}
}

// TestBacktickFencePartialLineIsTrimmed: the reference reads the opening fence
// from the start of token.raw with /^(`{3,}|~{3,})/, so a backtick block's
// partial closing fence is trimmed like a tilde one. Pier's fenceStart saw the
// raw's last line as the opening line's info string and refused, leaving the
// fence characters in the block body.
func TestBacktickFencePartialLineIsTrimmed(t *testing.T) {
	blocks := Lex("```\ncode\n``")
	TrimPartialClosingFences(blocks)
	if want := "code"; blocks[0].Body != want {
		t.Fatalf("body = %q, want %q", blocks[0].Body, want)
	}
}

// TestClosedFenceClassifiedFromTheMarker pins that the opening fence is read
// from the raw's first line: a closed backtick block's closing line is not its
// opening line's info string, so the block is not reported open.
func TestClosedFenceClassifiedFromTheMarker(t *testing.T) {
	for _, raw := range []string{"```\ncode\n```", "~~~\ncode\n~~~"} {
		if codeBlockOpen(raw) {
			t.Errorf("%q classified open", raw)
		}
	}
	for _, raw := range []string{"```\ncode", "```\ncode\n``"} {
		if !codeBlockOpen(raw) {
			t.Errorf("%q classified closed, want open", raw)
		}
	}
}
