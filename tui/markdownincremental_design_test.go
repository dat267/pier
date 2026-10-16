package tui

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// Prototype of the incremental block lexer design. incrementalLexWithStarts
// returns the tokens (and their start offsets) for next given the tokens already
// computed for prev, when next extends prev. It keeps the tokens before the last
// "open" block and re-lexes only the trailing run, instead of re-lexing the
// whole document.
//
// The correctness contract: the result must be DeepEqual to LexMarkdown(next)
// for every prefix of a document fed one byte at a time (see the tests below).
func incrementalLexWithStarts(prev string, prevTokens []*MdToken, prevStarts []int, next string) ([]*MdToken, []int) {
	if len(prevTokens) == 0 || len(prevTokens) != len(prevStarts) || !strings.HasPrefix(next, prev) {
		return lexMarkdownWithStarts(next)
	}
	// Re-lex from the start of the trailing run of tokens that appending can
	// still affect. The last token always can. A preceding token is pulled in
	// when it is an open block that can merge forward, or when the token we are
	// about to re-lex ends mid-line: an unterminated final line can turn a
	// previous block's terminator into content (a whitespace-only partial line
	// is lexed as `space`, but appending a word makes it a paragraph
	// continuation).
	start := len(prevTokens) - 1
	for start > 0 && (lexTailExtendable(prevTokens[start-1]) || !strings.HasSuffix(prevTokens[start].Raw, "\n")) {
		start--
	}
	offset := prevStarts[start]
	suffixTokens, suffixStarts := lexMarkdownWithStarts(next[offset:])
	out := make([]*MdToken, 0, start+len(suffixTokens))
	out = append(out, prevTokens[:start]...)
	out = append(out, suffixTokens...)
	outStarts := make([]int, 0, start+len(suffixStarts))
	outStarts = append(outStarts, prevStarts[:start]...)
	for _, s := range suffixStarts {
		outStarts = append(outStarts, offset+s)
	}
	return out, outStarts
}

// lexTailExtendable reports whether appending text after t can change t itself
// or merge it with the appended block.
func lexTailExtendable(t *MdToken) bool {
	switch t.Type {
	case "space":
		// A blank line inside or before a list, blockquote, indented code block
		// or unclosed fence does not end it, so the next block can merge back.
		return true
	case "list", "blockquote":
		return true
	case "code":
		// Indented code continues past blank lines; an unclosed fence absorbs
		// everything after it. A closed fence is final.
		return !fenceClosed(t.Raw)
	case "latexBlock":
		return t.Pending
	}
	return false
}

// fenceClosed reports whether a fenced code token's raw ends with a closing
// fence line. Indented code (no opening fence) is treated as open.
func fenceClosed(raw string) bool {
	trimmed := strings.TrimRight(raw, "\n")
	last := trimmed
	if nl := strings.LastIndexByte(trimmed, '\n'); nl >= 0 {
		last = trimmed[nl+1:]
	}
	return mdFenceEndRegex.MatchString(last)
}

// fullLexStarts is the reference offset list for a full lex, for comparison.
func fullLexStarts(source string) []int {
	_, starts := lexMarkdownWithStarts(source)
	return starts
}

func tokenTypes(tokens []*MdToken) []string {
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		out[i] = tok.Type
	}
	return out
}

// TestIncrementalLexMatchesFullLexByteByByte feeds the sample one byte at a
// time and requires every step to match a full lex exactly.
func TestIncrementalLexMatchesFullLexByteByByte(t *testing.T) {
	const text = incrementalSample
	tokens := []*MdToken{}
	starts := []int{}
	prev := ""
	for n := 1; n <= len(text); n++ {
		next := text[:n]
		tokens, starts = incrementalLexWithStarts(prev, tokens, starts, next)
		prev = next
		want := LexMarkdown(next)
		if !reflect.DeepEqual(tokens, want) {
			t.Fatalf("prefix %d:\n got %#v\nwant %#v", n, tokenTypes(tokens), tokenTypes(want))
		}
		if got, wantStarts := starts, fullLexStarts(next); !reflect.DeepEqual(got, wantStarts) {
			t.Fatalf("prefix %d: starts %v want %v", n, got, wantStarts)
		}
	}
}

// TestIncrementalLexMatchesFullLexRandomAppends appends random chunks of the
// corpus and requires the same token stream.
func TestIncrementalLexMatchesFullLexRandomAppends(t *testing.T) {
	sources := []string{incrementalSample, benchMixed, benchProse}
	rng := rand.New(rand.NewSource(1))
	for _, source := range sources {
		tokens := []*MdToken{}
		starts := []int{}
		prev := ""
		for pos := 0; pos < len(source); {
			step := 1 + rng.Intn(7)
			if pos+step > len(source) {
				step = len(source) - pos
			}
			next := source[:pos+step]
			tokens, starts = incrementalLexWithStarts(prev, tokens, starts, next)
			prev = next
			pos += step
			want := LexMarkdown(next)
			if !reflect.DeepEqual(tokens, want) {
				t.Fatalf("pos %d: token mismatch", pos)
			}
			if got, wantStarts := starts, fullLexStarts(next); !reflect.DeepEqual(got, wantStarts) {
				t.Fatalf("pos %d: starts %v want %v", pos, got, wantStarts)
			}
		}
	}
}

// TestIncrementalLexReusesStablePrefix pins the win: appending to a finished
// block must not re-lex or re-allocate the earlier tokens.
func TestIncrementalLexReusesStablePrefix(t *testing.T) {
	prev := strings.Repeat("Paragraph with **bold** and `code`.\n\n", 100)
	tokens, starts := lexMarkdownWithStarts(prev)
	first := tokens[0]
	next := prev + "A new paragraph.\n"
	got, _ := incrementalLexWithStarts(prev, tokens, starts, next)
	if got[0] != first {
		t.Fatal("stable prefix token was re-allocated")
	}
	if len(got) <= len(tokens) {
		t.Fatalf("tokens = %d, want more than %d", len(got), len(tokens))
	}
}

func growingPrefixes(lines int) []string {
	prefixes := make([]string, lines)
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "Paragraph %d with **bold** and `code` and a [link](https://example.com).\n\n", i)
		prefixes[i] = sb.String()
	}
	return prefixes
}

func benchmarkIncrementalStream(b *testing.B, prefixes []string) {
	b.ReportAllocs()
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		var tokens []*MdToken
		var starts []int
		prev := ""
		for _, p := range prefixes {
			tokens, starts = incrementalLexWithStarts(prev, tokens, starts, p)
			prev = p
		}
	}
}

func BenchmarkIncrementalLexStream500(b *testing.B) {
	benchmarkIncrementalStream(b, growingPrefixes(500))
}

func BenchmarkIncrementalLexStream2000(b *testing.B) {
	benchmarkIncrementalStream(b, growingPrefixes(2000))
}

func listStreamPrefixes(items int) []string {
	prefixes := make([]string, items)
	var sb strings.Builder
	for i := 0; i < items; i++ {
		fmt.Fprintf(&sb, "- item %d with some text and `code`\n", i)
		prefixes[i] = sb.String()
	}
	return prefixes
}

// BenchmarkIncrementalLexListStream streams a single growing list, the shape
// where the open-block re-lex is largest.
func BenchmarkIncrementalLexListStream(b *testing.B) {
	benchmarkIncrementalStream(b, listStreamPrefixes(500))
}

// BenchmarkFullLexListStream is the current cost for the same list stream.
func BenchmarkFullLexListStream(b *testing.B) {
	prefixes := listStreamPrefixes(500)
	b.ReportAllocs()
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		for _, p := range prefixes {
			_ = LexMarkdown(p)
		}
	}
}

// TestIncrementalLexRetroactiveConstructs covers the cases where appending text
// changes a block that a previous full lex had already closed.
func TestIncrementalLexRetroactiveConstructs(t *testing.T) {
	documents := []string{
		"Paragraph\n---\n",                  // setext heading forms late
		"a\n===\n\nnext\n",                  // setext then paragraph
		"| a | b |\n|---|---|\n| 1 | 2 |\n", // table delimiter completes the header
		"- a\n\n- b\n",                      // loose list across a blank line
		"- a\n  continued\n",                // lazy list continuation
		"> a\n> b\n",                        // blockquote accumulates lines
		"    code\n    more\n",              // indented code
		"```go\nx\n```\n\ntail\n",           // closed fence then a paragraph
		"```go\nx\n",                        // unclosed fence
		"$$x",                               // pending latex
		"# h\n\n> q\n\n- l\n\n```\nc\n```\n\np\n",
		"a\n\n\n\n- b\n",                // multiple blank lines then list
		"text\n\n<div>\nhtml\n</div>\n", // html block
		"- a\n\n  b\n",                  // list item continued by indent
	}
	for _, doc := range documents {
		t.Run(doc, func(t *testing.T) {
			var tokens []*MdToken
			var starts []int
			prev := ""
			for n := 1; n <= len(doc); n++ {
				next := doc[:n]
				tokens, starts = incrementalLexWithStarts(prev, tokens, starts, next)
				prev = next
				if want := LexMarkdown(next); !reflect.DeepEqual(tokens, want) {
					t.Fatalf("prefix %d %q:\n got %v\nwant %v", n, next, tokenTypes(tokens), tokenTypes(want))
				}
			}
		})
	}
}

// FuzzIncrementalLexMatchesFull feeds arbitrary input in small chunks and
// requires the incremental token stream to match a full lex at every step.
func FuzzIncrementalLexMatchesFull(f *testing.F) {
	for _, seed := range []string{
		"# heading\n\nparagraph with **bold** and `code`\n",
		"- [ ] task\n- item\n\n> quote\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"```go\nfmt.Println(\"x\")\n```\n",
		"$$\\frac{a}{b}$$\n",
		"paragraph\n===\n\n- a\n\n- b\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		var tokens []*MdToken
		var starts []int
		prev := ""
		for pos := 0; pos < len(source); {
			step := 1 + pos%5
			if pos+step > len(source) {
				step = len(source) - pos
			}
			next := source[:pos+step]
			tokens, starts = incrementalLexWithStarts(prev, tokens, starts, next)
			prev = next
			pos += step
			want := LexMarkdown(next)
			if !reflect.DeepEqual(tokens, want) {
				t.Fatalf("prefix %d (%q): mismatch\n got %v\nwant %v", pos, next, tokenTypes(tokens), tokenTypes(want))
			}
		}
	})
}
