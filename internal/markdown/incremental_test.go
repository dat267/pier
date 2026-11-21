package markdown

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

const incrementalSample = `# Title

Intro paragraph with **bold**, ` + "`code`" + ` and a [link](https://example.com).

- item one
- item two

> a quote
> second line

` + "```go" + `
func main() {
	fmt.Println("hi")
}
` + "```" + `

| a | b |
| - | - |
| 1 | 2 |

Final paragraph that keeps growing.`

// TestIncrementalMatchesFullLexByteByByte feeds the sample one byte at a time
// and requires the incremental blocks to equal a fresh lex at every prefix.
func TestIncrementalMatchesFullLexByteByByte(t *testing.T) {
	doc := NewDocument()
	for n := 1; n <= len(incrementalSample); n++ {
		prefix := incrementalSample[:n]
		got := doc.Update(prefix)
		want := Lex(prefix)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("prefix %d:\n got %#v\nwant %#v", n, blockKinds(got), blockKinds(want))
		}
	}
}

func TestIncrementalMatchesFullLexRandomAppends(t *testing.T) {
	sources := []string{incrementalSample, benchMixed(8), benchProse(40), benchDoc(8 << 10)}
	rng := rand.New(rand.NewSource(1))
	for _, source := range sources {
		doc := NewDocument()
		for pos := 0; pos < len(source); {
			step := 1 + rng.Intn(9)
			if pos+step > len(source) {
				step = len(source) - pos
			}
			got := doc.Update(source[:pos+step])
			if want := Lex(source[:pos+step]); !reflect.DeepEqual(got, want) {
				t.Fatalf("pos %d mismatch", pos+step)
			}
			pos += step
		}
	}
}

func TestIncrementalRetroactiveConstructs(t *testing.T) {
	documents := []string{
		"Paragraph\n---\n",
		"a\n===\n\nnext\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"- a\n\n- b\n",
		"- a\n  continued\n",
		"> a\n> b\n",
		"    code\n    more\n",
		"```go\nx\n```\n\ntail\n",
		"```go\nx\n",
		"$$x",
		"# h\n\n> q\n\n- l\n\n```\nc\n```\n\np\n",
		"a\n\n\n\n- b\n",
		"text\n\n<div>\nhtml\n</div>\n",
		"- a\n\n  b\n",
	}
	for _, document := range documents {
		t.Run(document, func(t *testing.T) {
			doc := NewDocument()
			for n := 1; n <= len(document); n++ {
				got := doc.Update(document[:n])
				if want := Lex(document[:n]); !reflect.DeepEqual(got, want) {
					t.Fatalf("prefix %d:\n got %v\nwant %v", n, blockKinds(got), blockKinds(want))
				}
			}
		})
	}
}

func TestIncrementalReusesStablePrefix(t *testing.T) {
	prev := strings.Repeat("Paragraph with **bold** and `code`.\n\n", 100)
	doc := NewDocument()
	blocks := doc.Update(prev)
	first := blocks[0]
	next := doc.Update(prev + "A new paragraph.\n")
	if next[0] != first {
		t.Fatal("stable prefix block was re-lexed")
	}
	if len(next) <= len(blocks) {
		t.Fatalf("blocks = %d, want more than %d", len(next), len(blocks))
	}
}

// FuzzIncrementalMatchesFull feeds arbitrary input in small chunks.
func FuzzIncrementalMatchesFull(f *testing.F) {
	for _, seed := range []string{
		"# heading\n\nparagraph with **bold** and `code`\n",
		"- [ ] task\n- item\n\n> quote\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"```go\nfmt.Println(\"x\")\n```\n",
		"paragraph\n===\n\n- a\n\n- b\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		doc := NewDocument()
		// The empty source is a real step: it lexes to no block, and the first
		// append after that is the case that used to index an empty slice. The
		// loop below starts at a one-byte prefix, so without this the fuzzer
		// never reaches it.
		if got := doc.Update(""); len(got) != 0 {
			t.Fatalf("empty source lexed to %d blocks", len(got))
		}
		for pos := 0; pos < len(source); {
			step := 1 + pos%5
			if pos+step > len(source) {
				step = len(source) - pos
			}
			got := doc.Update(source[:pos+step])
			if want := Lex(source[:pos+step]); !reflect.DeepEqual(got, want) {
				t.Fatalf("prefix %d (%q) mismatch", pos, source[:pos+step])
			}
			pos += step
		}
	})
}

func blockKinds(blocks []*Block) []BlockKind {
	out := make([]BlockKind, len(blocks))
	for i, b := range blocks {
		out[i] = b.Kind
	}
	return out
}

func benchmarkIncrementalRender(b *testing.B, prefixes []string) {
	b.ReportAllocs()
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		doc := NewDocument()
		for _, p := range prefixes {
			doc.Update(p)
		}
	}
}

func growingParagraphPrefixes(lines int) []string {
	prefixes := make([]string, lines)
	var sb strings.Builder
	for i := range lines {
		sb.WriteString("Paragraph with **bold** and `code` and a [link](https://example.com).\n\n")
		prefixes[i] = sb.String()
	}
	return prefixes
}

func BenchmarkIncrementalRender500(b *testing.B) {
	benchmarkIncrementalRender(b, growingParagraphPrefixes(500))
}

func BenchmarkFullRender500(b *testing.B) {
	prefixes := growingParagraphPrefixes(500)
	b.ReportAllocs()
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		for _, p := range prefixes {
			_ = Lex(p)
		}
	}
}

// TestLexFromMatchesLex requires the tail entry point to produce exactly the
// blocks a full lex produces, offsets included, for every stable boundary of a
// mixed document.
func TestLexFromMatchesLex(t *testing.T) {
	full := Lex(incrementalSample)
	src := incrementalSample
	for start := 1; start < len(full); start++ {
		from := full[start].Start
		tail := LexFrom(src, from)
		want := full[start:]
		if !reflect.DeepEqual(tail, want) {
			t.Fatalf("LexFrom(%d) mismatch: got %v want %v", from, blockKinds(tail), blockKinds(want))
		}
	}
}

func TestFirstUnstableMarksOnlyExtendableBlocks(t *testing.T) {
	src := "# Title\n\none\n\ntwo\n\nthree\n"
	blocks := Lex(src)
	first := FirstUnstable(blocks, src)
	// The trailing blank line is a space block, which an append can always
	// extend, so the boundary is at most the last two blocks.
	if first < len(blocks)-2 || first > len(blocks)-1 {
		t.Fatalf("FirstUnstable = %d, want %d..%d", first, len(blocks)-2, len(blocks)-1)
	}
	for i := range first {
		if got := LexFrom(src, blocks[i].Start); len(got) == 0 {
			t.Fatalf("stable block %d re-lexes empty", i)
		}
	}

	// An unclosed fence keeps both it and any preceding blank line unstable.
	open := "text\n\n```go\nx\n"
	openBlocks := Lex(open)
	if got := FirstUnstable(openBlocks, open); got != 1 {
		t.Fatalf("FirstUnstable of an open fence = %d, want 1", got)
	}
}

// A pending display-math block reaches the end of the source
// (markdown.ts tokenizeBlockLatex: `^ {0,3}\$\$…([\s\S]*)$` plus
// looksLikePendingDollarMath), so text appended after blank lines can turn a
// closed `$$` paragraph into one. An incremental update must re-lex from that
// paragraph instead of keeping it.
func TestIncrementalPendingLatexBlockMatchesFullLex(t *testing.T) {
	for _, source := range []string{
		"$$\nx\n\n*a **b** c*",
		"$$\nx\n\na *b*",
		"\\[\nx\n\n*a*",
		"$$\n\\alpha\n\n*a*",
	} {
		doc := NewDocument()
		if got := doc.Update(""); len(got) != 0 {
			t.Fatalf("empty source lexed to %d blocks", len(got))
		}
		for n := 1; n <= len(source); n++ {
			if got, want := doc.Update(source[:n]), Lex(source[:n]); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q prefix %d:\n got %v\nwant %v", source, n, blockKinds(got), blockKinds(want))
			}
		}
	}
}

// A setext underline merges a paragraph with the lines an interrupting block
// already claimed (block_rest.go lheadingPast, marked 18.0.11's lheading before
// its paragraph rule), so such a paragraph is not stable until the underline
// arrives.
func TestIncrementalSetextHeadingAbsorbsAnInterruptingLine(t *testing.T) {
	for _, source := range []string{
		"a\n<!--\n=",
		"a\n<!--\n===",
		"| a |\n<!--\n=",
		"x\n$$y$$\n-",
		"z\n$$x$$\n- ",
	} {
		doc := NewDocument()
		if got := doc.Update(""); len(got) != 0 {
			t.Fatalf("empty source lexed to %d blocks", len(got))
		}
		for n := 1; n <= len(source); n++ {
			if got, want := doc.Update(source[:n]), Lex(source[:n]); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q prefix %d:\n got %v\nwant %v", source, n, blockKinds(got), blockKinds(want))
			}
		}
	}
}

// A growing document must lex to the same blocks as a fresh full lex, at every
// intermediate length. Reference: the incremental pass exists only as a fast
// path (no upstream counterpart), so the invariant is local: Document.Update is
// required to agree with Lex on the same source.
func TestDocumentUpdateMatchesFullLex(t *testing.T) {
	pieces := []string{
		"# h\n", "para\n", "\n", "- item\n", "1. one\n", "> quote\n",
		"```\ncode\n```\n", "| a | b |\n|---|---|\n| 1 | 2 |\n",
		"[ref]: /u\n", "- [ ] task\n", "text `code` more\n", "  indented\n",
		"***\n", "> - nested\n", "\t\ttab\n", "a [ref] b\n",
	}
	rng := rand.New(rand.NewSource(11))
	compare := func(src string, got, want []*Block) {
		if len(got) != len(want) {
			t.Fatalf("src=%q: block count %d != %d\nwant=%q\ngot=%q", src, len(want), len(got), raws(want), raws(got))
		}
		for i := range want {
			if got[i].Kind != want[i].Kind || got[i].Raw != want[i].Raw ||
				got[i].Start != want[i].Start || got[i].End != want[i].End {
				t.Fatalf("src=%q block %d: got {%v %q %d-%d} want {%v %q %d-%d}",
					src, i, got[i].Kind, got[i].Raw, got[i].Start, got[i].End,
					want[i].Kind, want[i].Raw, want[i].Start, want[i].End)
			}
		}
	}
	for iter := 0; iter < 400; iter++ {
		n := 1 + rng.Intn(10)
		doc := &Document{}
		src := ""
		for i := 0; i < n; i++ {
			src += pieces[rng.Intn(len(pieces))]
			got := doc.Update(src)
			compare(src, got, Lex(src))
		}
	}
}

func raws(blocks []*Block) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Raw)
	}
	return out
}

// D505: Update on a document whose previous source lexed to no block panicked.
// `FirstUnstable` starts at len(blocks)-1, so an empty block list yields start 0
// (it clamps negatives), and Update then read `d.blocks[start].Start` on an empty
// slice. Reachable the moment a stream starts: the first non-empty delta after an
// already-empty update hits it.
func TestDocumentUpdateFromAnEmptyLexDoesNotPanic(t *testing.T) {
	doc := NewDocument()
	if blocks := doc.Update(""); len(blocks) != 0 {
		t.Fatalf("empty source lexed to %d blocks", len(blocks))
	}
	blocks := doc.Update("a")
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	// And the result equals a fresh lex of the same source.
	fresh := NewDocument().Update("a")
	if len(fresh) != 1 || fresh[0].Kind != blocks[0].Kind || fresh[0].Raw != blocks[0].Raw {
		t.Fatalf("incremental %+v != fresh %+v", blocks[0], fresh[0])
	}
}
