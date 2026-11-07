package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. marked's code tokenizer
// runs the raw through `te`: trailing lines that are blank or whitespace-only
// are cut from the raw when two or more of them follow the last content line
// (the piece after a final newline counts), and the cut lines then start a
// space token at top level. A single trailing newline stays in the text
// (`    indented\n` keeps `text: "indented\n"`), and the text never ends in a
// second newline.
func TestIndentedCodeTrailingBlankLines(t *testing.T) {
	for _, testCase := range []struct {
		src   string
		want  string
		after string
	}{
		// A whitespace-only trailing line plus the final newline counts as two
		// blankish pieces: both leave the block.
		{"    code\n \n", `code:"code"`, ` | space`},
		{"\t- t\n \n", `code:"- t"`, ` | space`},
		// Empty trailing lines (more than one piece) leave the block too.
		{"    a\n\n\n", `code:"a"`, ` | space`},
		{"    a\n\t\n", `code:"a"`, ` | space`},
		// A single trailing newline stays in the text.
		{"    indented\n", `code:"indented\n"`, ``},
		// One blankish line before a following paragraph stays in the text.
		{"    a\n \ntext\n", `code:"a"`, ` | space | p:"text"`},
		// Content lines in the middle keep their newlines.
		{"    a\n    b\n", `code:"a\nb\n"`, ``},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want+testCase.after {
			t.Errorf("%q:\n got %s\nwant %s%s", testCase.src, got, testCase.want, testCase.after)
		}
	}
}
