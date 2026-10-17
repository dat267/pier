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
	for i := 0; i < lines; i++ {
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
	for i := 0; i < first; i++ {
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
