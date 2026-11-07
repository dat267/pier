package markdown

import (
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11 def rule, which upstream
// packages/tui/src/components/markdown.ts lexes with:
//
//	^ {0,3}\[label\]: *(?:\n[ \t]*)?([^<\s][^\s]*|<.*?>)
//	  (?:(?: +(?:\n[ \t]*)?| *\n[ \t]*)(title))? *(?:\n+|$)
//
// A definition produces a def token that renders nothing. pier showed the line
// as paragraph text instead.
func TestLinkDefinitionRendersNothing(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		want string
	}{
		{"[x]: /u\n* a\n", `def:"x" | ul[(p:"a")]`},
		{"[x]: /u\ntext\n", `def:"x" | p:"text"`},
		{"[x]: /u\n;text\n", `def:"x" | p:";text"`},
		// The href may sit on the next line and a title after another one.
		{"[x]:\n/u\nnext\n", `def:"x" | p:"next"`},
		{"[x]: /u\n\"T\"\nnext\n", `def:"x" | p:"next"`},
		// An angle href loses the brackets and cannot span lines.
		{"[x]: <u>\n", `def:"x"`},
		{"[x]: <a\nb>\n", `p:"[x]: <a\nb>"`},
		// Titles are quoted; an unquoted word makes the whole line a paragraph.
		{"[x]: /u \"T\"\n", `def:"x"`},
		{"[x]: /u title\ntext\n", `p:"[x]: /u title\ntext"`},
		// The label may contain spaces, including a trailing one.
		{"[x y]: /u\n", `def:"x y"`},
		{"[x y ]: /u\n", `def:"x y "`},
		// A label that is whitespace only or holds brackets is a paragraph.
		{"[  ]: /u\n", `p:"[  ]: /u"`},
		{"[x][y]: /u\n", `p:"[x][y]: /u"`},
		// Only spaces may follow the colon before the target.
		{"[x]: \t/u\n", `p:"[x]: \t/u"`},
		{"[x]:\n\n/u\n", `p:"[x]:" | space | p:"/u"`},
		// Inside a paragraph the definition line is text.
		{"a\n[x]: /u\nb\n", `p:"a\n[x]: /u\nb"`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}

// TestLinkDefinitionStaysIdenticalInAStream pins the incremental contract:
// a def is extendable (a later quoted line can become its title), so appends
// cannot strand it half-parsed.
func TestLinkDefinitionStaysIdenticalInAStream(t *testing.T) {
	source := "[x]: /u\n* a\n"
	doc := NewDocument()
	for n := 1; n <= len(source); n++ {
		got := doc.Update(source[:n])
		if want := Lex(source[:n]); defShapes(got) != defShapes(want) {
			t.Fatalf("prefix %d:\n got %s\nwant %s", n, defShapes(got), defShapes(want))
		}
	}
}

// defShapes projects def blocks; everything else is compared by shape only.
func defShapes(blocks []*Block) string {
	out := ""
	for _, b := range blocks {
		switch b.Kind {
		case BlockLinkDef:
			out += "def:" + b.Text + "|"
		case BlockList, BlockQuote:
			out += "(" + defShapes(b.Blocks) + ")"
		default:
			out += "."
		}
	}
	return out
}
