package markdown

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) renders highlight-independent Markdown through
// Marked 18.0.11 (packages/tui/src/components/markdown.ts), so a link
// definition, its label normalization and the reference forms a document may use
// resolve exactly as Marked's `def`/`ref` rules do. The expectations below were
// captured from the pinned Marked by the differential harness described in the
// audit notes and are limited to the drawing-relevant projection.
//
// The previous corpus had no definition or reference-link case at all, which is
// why internal/markdown/links.go was the least-covered file in the package.
func TestReferenceLinkResolutionMatchesMarked(t *testing.T) {
	// Source normalizes CRLF and tabs before lexing, as markdown.ts does.
	normalize := func(src string) string {
		src = strings.ReplaceAll(src, "\r\n", "\n")
		src = strings.ReplaceAll(src, "\r", "\n")
		return strings.ReplaceAll(src, "\t", "   ")
	}
	for _, test := range []struct{ source, want string }{
		{"[a]: /url\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[A]: /url\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[ a ]: /url\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","type":"text"}],"type":"paragraph"}]`},
		{"[a]: <>\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[a]: /url \"t\"\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/url","text":"a","title":"t","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[a]: /url\n\"title\"\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/url","text":"a","title":"title","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[a]: /url\n[b]: /b\n\n[a] [b]\n", `[{"type":"space"},{"text":"[a] [b]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"},{"text":" ","type":"text"},{"href":"/b","text":"b","tokens":[{"text":"b","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[a]: /first\n[a]: /second\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/first","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[a][]\n\n[a]: /url\n", `[{"text":"[a][]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"},{"type":"space"}]`},
		{"[a][b]\n\n[b]: /url\n", `[{"text":"[a][b]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"},{"type":"space"}]`},
		{"![img]\n\n[img]: /i.png\n", `[{"text":"![img]","tokens":[{"href":"/i.png","text":"img","tokens":[{"text":"img","type":"text"}],"type":"image"}],"type":"paragraph"},{"type":"space"}]`},
		{"![img][]\n\n[img]: /i.png \"t\"\n", `[{"text":"![img][]","tokens":[{"href":"/i.png","text":"img","title":"t","tokens":[{"text":"img","type":"text"}],"type":"image"}],"type":"paragraph"},{"type":"space"}]`},
		{"[a]: /url\n\n[a]\n[b]\n", `[{"type":"space"},{"text":"[a]\n[b]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"},{"text":"\n[b]","type":"text"}],"type":"paragraph"}]`},
		{"[a]: /url  x\n\n[a]\n", `[{"text":"[a]: /url  x","tokens":[{"text":"[a]: /url  x","type":"text"}],"type":"paragraph"},{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","type":"text"}],"type":"paragraph"}]`},
		{"[a]: /ur l\n\n[a]\n", `[{"text":"[a]: /ur l","tokens":[{"text":"[a]: /ur l","type":"text"}],"type":"paragraph"},{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","type":"text"}],"type":"paragraph"}]`},
		{"> [a]: /url\n\n> [a]\n", `[{"text":"[a]: /url","tokens":[],"type":"blockquote"},{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}],"type":"blockquote"}]`},
		{"- [a]: /url\n\n[a]\n", `[{"items":[{"text":"[a]: /url","tokens":[],"type":"list_item"}],"type":"list"},{"type":"space"},{"text":"[a]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"# [a]: /url\n\n[a]\n", `[{"depth":1,"text":"[a]: /url","tokens":[{"text":"[a]: /url","type":"text"}],"type":"heading"},{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","type":"text"}],"type":"paragraph"}]`},
		{"```\n[a]: /url\n```\n\n[a]\n", `[{"raw":"` + "`" + `` + "`" + `` + "`" + `\n[a]: /url\n` + "`" + `` + "`" + `` + "`" + `","text":"[a]: /url","type":"code"},{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","type":"text"}],"type":"paragraph"}]`},
		{"    [a]: /url\n\n[a]\n", `[{"raw":"    [a]: /url","text":"[a]: /url","type":"code"},{"type":"space"},{"text":"[a]","tokens":[{"text":"[a]","type":"text"}],"type":"paragraph"}]`},
		{"[a*em][r]\n\n[r]: /u\n", `[{"text":"[a*em][r]","tokens":[{"href":"/u","text":"a*em","tokens":[{"text":"a*em","type":"text"}],"type":"link"}],"type":"paragraph"},{"type":"space"}]`},
		{"[a]: /url?q=1&r=2\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/url?q=1&r=2","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[a]: /u\\*rl\n\n[a]\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/u*rl","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
		{"[&amp;]: /u\n\n[&]\n", `[{"type":"space"},{"text":"[&]","tokens":[{"text":"[&]","type":"text"}],"type":"paragraph"}]`},
		{"[a]b]: /url\n\n[a]b]\n", `[{"text":"[a]b]: /url","tokens":[{"text":"[a]b]: /url","type":"text"}],"type":"paragraph"},{"type":"space"},{"text":"[a]b]","tokens":[{"text":"[a]b]","type":"text"}],"type":"paragraph"}]`},
		{"text [a] more\n\n[a]: /u\n", `[{"text":"text [a] more","tokens":[{"text":"text ","type":"text"},{"href":"/u","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"},{"text":" more","type":"text"}],"type":"paragraph"},{"type":"space"}]`},
		{"[a]: /url\r\n\r\n[a]\r\n", `[{"type":"space"},{"text":"[a]","tokens":[{"href":"/url","text":"a","tokens":[{"text":"a","type":"text"}],"type":"link"}],"type":"paragraph"}]`},
	} {
		got := stableValue(projectBlocks(withoutLinkDefs(goldenBlocks(normalize(test.source)))))
		if got != test.want {
			t.Errorf("%q\n got %s\nwant %s", test.source, got, test.want)
		}
	}
}

// withoutLinkDefs drops definition blocks: Marked emits a `def` token that has
// no rendering, and the expectations below record only drawing-relevant blocks.
func withoutLinkDefs(blocks []*Block) []*Block {
	out := make([]*Block, 0, len(blocks))
	for _, block := range blocks {
		if block.Kind == BlockLinkDef {
			continue
		}
		block.Blocks = withoutLinkDefs(block.Blocks)
		out = append(out, block)
	}
	return out
}
