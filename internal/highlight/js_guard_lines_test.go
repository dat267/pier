package highlight

import (
	"strings"
	"testing"
)

// The JavaScript guards decide whether an identifier is an object attribute,
// which depends on the document's line-terminator class: with CR/U+2028/U+2029
// present the reference's ^/$/dot semantics apply. M24 moved that decision from
// a per-call tail rescan to the flag the highlighter already derived, so both
// classes must keep the pinned output.
func TestJavaScriptGuardsRespectDocumentLineClass(t *testing.T) {
	for name, line := range map[string]string{"lf": "\n", "crlf": "\r\n", "u2028": "\u2028"} {
		t.Run(name, func(t *testing.T) {
			src := "const o = {key: 1};" + line
			result, err := Highlight(src, "js")
			if err != nil {
				t.Fatal(err)
			}
			html := RenderHTML(result.Nodes)
			if !strings.Contains(html, `class="hljs-attr">key</span>`) {
				t.Fatalf("object attribute not recognised in %s document: %s", name, html)
			}
		})
	}
}

func BenchmarkJavaScriptObjectAttributes(b *testing.B) {
	src := strings.Repeat("const x = (a) => ({k: \"v\", n: 1}); // comment\n", 1600)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, err := Highlight(src, "js"); err != nil {
			b.Fatal(err)
		}
	}
}
