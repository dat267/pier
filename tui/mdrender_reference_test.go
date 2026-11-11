package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// TestMarkdownRenderMatchesReference pins the rendered output of the markdown
// component against the pin, not against a hand-written expectation. Each case
// below was produced by pi v1.5.0 (f9a26365)
// packages/tui/src/components/markdown.ts: the Markdown class constructed as
// (text, 0, 0, identityTheme) and rendered at the recorded width, with every
// theme function the identity so the lines carry no styling. The corpus is
// tui/testdata/md_corpus.json, whose token projection md_golden.txt already
// pins the parse; this pins the layout: indentation, borders, blank-line
// spacing, list bullets and the wrapping of every block.
//
// The five cjk-* documents are covered by md_golden.txt instead: the pinned
// checkout does not install marked-cjk-friendly, and its CJK emphasis
// tokenizer is what decides those renders.
func TestMarkdownRenderMatchesReference(t *testing.T) {
	identity := func(text string) string { return text }
	theme := MarkdownTheme{
		Heading: identity, Link: identity, LinkURL: identity, Code: identity,
		CodeBlock: identity, CodeBlockBorder: identity, Quote: identity,
		QuoteBorder: identity, Hr: identity, ListBullet: identity,
		Bold: identity, Italic: identity, Strikethrough: identity, Underline: identity,
	}
	cases := []struct {
		name  string
		width int
		want  []string
	}{
		{
			name:  "blockquote",
			width: 40,
			want: []string{
				`│ quoted                                `,
				`│ more                                  `,
			},
		},
		{
			name:  "blockquote-nested",
			width: 40,
			want: []string{
				`│ │ deep                                `,
			},
		},
		{
			name:  "list-in-quote",
			width: 40,
			want: []string{
				`│ 1. a                                  `,
				`│ 2. b                                  `,
			},
		},
		{
			name:  "quote-code",
			width: 40,
			want: []string{
				"\u2502 ```                                   ",
				`│   x                                   `,
				"\u2502 ```                                   ",
			},
		},
		{
			name:  "fence",
			width: 40,
			want: []string{
				"```js                                   ",
				`  code();                               `,
				"```                                     ",
			},
		},
		{
			name:  "fence-nolang",
			width: 40,
			want: []string{
				"```                                     ",
				`  plain                                 `,
				"```                                     ",
			},
		},
		{
			name:  "code-then-text",
			width: 40,
			want: []string{
				"```                                     ",
				`  x                                     `,
				"```                                     ",
				`                                        `,
				`text                                    `,
			},
		},
		{
			name:  "ul",
			width: 40,
			want: []string{
				`- a                                     `,
				`- b                                     `,
			},
		},
		{
			name:  "nested-list",
			width: 40,
			want: []string{
				`- a                                     `,
				`    - b                                 `,
				`- c                                     `,
			},
		},
		{
			name:  "ol",
			width: 40,
			want: []string{
				`1. one                                  `,
				`2. two                                  `,
			},
		},
		{
			name:  "task-list",
			width: 40,
			want: []string{
				`- [ ] todo                              `,
				`- [x] done                              `,
			},
		},
		{
			name:  "nested-list",
			width: 20,
			want: []string{
				`- a                 `,
				`    - b             `,
				`- c                 `,
			},
		},
		{
			name:  "table",
			width: 40,
			want: []string{
				`┌───┬───┐                               `,
				`│ a │ b │                               `,
				`├───┼───┤                               `,
				`│ 1 │ 2 │                               `,
				`└───┴───┘                               `,
			},
		},
		{
			name:  "table-no-pipes",
			width: 40,
			want: []string{
				`┌───┬───┐                               `,
				`│ a │ b │                               `,
				`├───┼───┤                               `,
				`│ 1 │ 2 │                               `,
				`└───┴───┘                               `,
			},
		},
		{
			name:  "table",
			width: 20,
			want: []string{
				`┌───┬───┐           `,
				`│ a │ b │           `,
				`├───┼───┤           `,
				`│ 1 │ 2 │           `,
				`└───┴───┘           `,
			},
		},
		{
			name:  "heading-inline",
			width: 40,
			want: []string{
				`A bold and code                         `,
			},
		},
		{
			name:  "paragraph-then-list",
			width: 40,
			want: []string{
				`text                                    `,
				`- a                                     `,
			},
		},
		{
			name:  "mixed",
			width: 40,
			want: []string{
				`H                                       `,
				`                                        `,
				`para b                                  `,
				`                                        `,
				`- item                                  `,
				`                                        `,
				`│ quote                                 `,
				`                                        `,
				"```py                                   ",
				`  x=1                                   `,
				"```                                     ",
				`                                        `,
				`┌───┐                                   `,
				`│ a │                                   `,
				`├───┤                                   `,
				`│ 1 │                                   `,
				`└───┘                                   `,
			},
		},
		{
			name:  "mixed",
			width: 20,
			want: []string{
				`H                   `,
				`                    `,
				`para b              `,
				`                    `,
				`- item              `,
				`                    `,
				`│ quote             `,
				`                    `,
				"```py               ",
				`  x=1               `,
				"```                 ",
				`                    `,
				`┌───┐               `,
				`│ a │               `,
				`├───┤               `,
				`│ 1 │               `,
				`└───┘               `,
			},
		},
	}
	corpus := loadMarkdownCorpus(t)
	for _, tc := range cases {
		source, ok := corpus[tc.name]
		if !ok {
			t.Fatalf("corpus has no case %q", tc.name)
		}
		got := NewMarkdown(source, 0, 0, theme, nil, MarkdownOptions{}).Render(tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("%s at width %d: %d lines, want %d:\ngot %q\nwant %q", tc.name, tc.width, len(got), len(tc.want), got, tc.want)
		}
		for index := range got {
			if got[index] != tc.want[index] {
				t.Errorf("%s at width %d line %d:\ngot  %q\nwant %q", tc.name, tc.width, index, got[index], tc.want[index])
			}
		}
	}
}

// loadMarkdownCorpus reads the shared markdown parity corpus.
func loadMarkdownCorpus(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("testdata/md_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	corpus := map[string]string{}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return corpus
}
