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

// Reference: pi v1.6.0 (faac5858) marked 18.0.11's def rule. When the trailing
// ` *(?:\n+|$)` fails after a title, the regex backtracks through the other
// closing quotes, then to no title, then to a longer angle href; the href and
// label use JavaScript's `\s`, and `<.*?>` stops at U+2028. `href` and `title`
// are the raw spans marked's cap[2] and cap[3] hold; rest is what follows.
func TestParseLinkDefBacktracksLikeTheRegex(t *testing.T) {
	for _, tc := range []struct {
		src, href, title, rest string
		ok                     bool
	}{
		{"[a]: /u\n't'x", "/u", "", "'t'x", true},
		{"[a]: /u\n\"t\"|b", "/u", "", "\"t\"|b", true},
		{"[a]: /u\n(t)x", "/u", "", "(t)x", true},
		{"[a]: /u\n't'\nx", "/u", "'t'", "x", true},
		{"[a]: /u \"t\" x", "", "", "", false},
		{"[a]: /u \"x\\\" y\"\n", "/u", "\"x\\\" y\"", "", true},
		{"[a]: /u \"x\\\"\n", "/u", "\"x\\\"", "", true},
		{"[a]: /u \"x\\\" y\" z\n", "", "", "", false},
		{"[a]: <x> y>\n", "x> y", "", "", true},
		{"[a]: <x>\u2028y>\n", "", "", "", false},
		{"[a]: /u\u00a0x\n", "", "", "", false},
		{"[\u00a0]: /u\n", "", "", "", false},
	} {
		_, href, title, end, ok := parseLinkDef(tc.src, 0)
		if ok != tc.ok || ok && (href != tc.href || title != tc.title || tc.src[end:] != tc.rest) {
			t.Errorf("parseLinkDef(%q) = %q, %q, rest %q, %v; want %q, %q, rest %q, %v",
				tc.src, href, title, tc.src[min(end, len(tc.src)):], ok, tc.href, tc.title, tc.rest, tc.ok)
		}
	}
}
