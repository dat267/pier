package markdown

import (
	"encoding/json"
	"reflect"
	"testing"
)

// pi v1.4.0 markdown.ts uses Marked 18's Lexer.inlineTokens. It masks escapes,
// code, HTML and links before the CJK-aware emStrong tokenizer scans closers.
func TestEmphasisMasksOpaqueInlineContent(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{"_`a_`", `[{"type":"text","text":"_"},{"type":"codespan","text":"a_"}]`},
		{`*a \**`, `[{"type":"em","text":"a \\*","tokens":[{"type":"text","text":"a "},{"type":"escape","raw":"\\*","text":"*"}]}]`},
		{"_<i title='_'>_", `[{"type":"em","text":"<i title='_'>","tokens":[{"type":"html","text":"<i title='_'>"}]}]`},
		{"*[x*y](z)*", `[{"type":"em","text":"[x*y](z)","tokens":[{"type":"link","href":"z","text":"x*y","tokens":[{"type":"text","text":"x*y"}]}]}]`},
	} {
		var want any
		if err := json.Unmarshal([]byte(item.want), &want); err != nil {
			t.Fatal(err)
		}
		got := projectInlines(ParseInline(item.input))
		if !reflect.DeepEqual(got, want) {
			encoded, _ := json.Marshal(got)
			t.Errorf("ParseInline(%q) = %s, want %s", item.input, encoded, item.want)
		}
	}
}
