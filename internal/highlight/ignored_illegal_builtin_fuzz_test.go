package highlight

import (
	"reflect"
	"testing"
)

// FuzzIgnoredIllegalBatchMatchesBuiltinScanner compares the batched scanner with
// the ordinary one on arbitrary input for the languages that use ignored
// character-class illegals (M140).
func FuzzIgnoredIllegalBatchMatchesBuiltinScanner(f *testing.F) {
	names := []string{"json", "javascript", "python", "typescript", "ruby", "php"}
	copies := make([]*compiledLanguage, len(names))
	for i, name := range names {
		copies[i] = languageWithoutIllegalBatch(f, name)
	}
	for _, seed := range []string{"0x0x", "xx xx [\"a\"]", "xx xx\r\n@x\u2028@😀", "/* x */ 0x", "xx xx \"unterminated"} {
		f.Add([]byte(seed), uint8(0), true)
	}
	f.Fuzz(func(t *testing.T, data []byte, selector uint8, ignore bool) {
		if len(data) > 512 {
			return
		}
		index := int(selector) % len(names)
		encoded, astral := encodeCodeUnits(string(data))
		want := highlightWith(copies[index], encoded, ignore)
		if astral {
			want.Nodes = decodeCodeUnitNodes(want.Nodes)
		}
		got, err := HighlightWith(string(data), names[index], ignore)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Nodes, want.Nodes) || got.Illegal != want.Illegal || got.Relevance != want.Relevance {
			t.Fatalf("input %q language %s ignore %v\ngot %q\nwant %q", data, names[index], ignore, RenderHTML(got.Nodes), RenderHTML(want.Nodes))
		}
	})
}
