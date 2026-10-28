package highlight

import (
	"encoding/json"
	"os"
	"testing"
	"unicode/utf8"
)

// pi v1.4.0 (4a080252) highlight.js compiles JavaScript regexes without /u,
// so matching advances by UTF-16 code units. The Node fixture pins both
// code-unit indices and the rendered HTML.
func TestJavaScriptCodeUnits(t *testing.T) {
	data, err := os.ReadFile("testdata/code-units/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Units []struct {
			Name, Source, Input string
			Indices             []int
		}
		Highlights []struct{ Name, Language, Code, HTML string }
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Units {
		t.Run("units/"+tc.Name, func(t *testing.T) {
			re, err := compilePattern(tc.Source, "m")
			if err != nil {
				t.Fatal(err)
			}
			encoded := encodeCodeUnitsString(tc.Input)
			match := re.FindStringSubmatchIndex(encoded)
			if got := codeUnitIndices(encoded, match); !equalInts(got, tc.Indices) {
				t.Fatalf("indices = %v; want %v", got, tc.Indices)
			}
		})
	}
	for _, tc := range fixture.Highlights {
		t.Run("highlight/"+tc.Name, func(t *testing.T) {
			result, err := Highlight(tc.Code, tc.Language)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.HTML {
				t.Fatalf("HTML = %q; want %q", got, tc.HTML)
			}
		})
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// decodedByteOffset maps an encoded byte offset to the UTF-8 byte length of
// the JavaScript prefix it denotes; a lone surrogate serializes as U+FFFD.
func decodedByteOffset(encoded string, offset int) int {
	raw := 0
	for i := 0; i < offset && i < len(encoded); {
		r, width := utf8.DecodeRuneInString(encoded[i:])
		unit := r - codeUnitBase
		if r > 0xFFFF && unit >= 0xD800 && unit <= 0xDFFF {
			if unit <= 0xDBFF && i+width < offset {
				next, nextWidth := utf8.DecodeRuneInString(encoded[i+width:])
				if nextUnit := next - codeUnitBase; next > 0xFFFF && nextUnit >= 0xDC00 && nextUnit <= 0xDFFF {
					raw += 4
					i += width + nextWidth
					continue
				}
			}
			raw += 3
			i += width
			continue
		}
		raw += width
		i += width
	}
	return raw
}

// codeUnitIndices converts encoded byte offsets into UTF-16 code-unit indexes.
func codeUnitIndices(encoded string, offsets []int) []int {
	if offsets == nil {
		return nil
	}
	out := make([]int, len(offsets))
	for i, offset := range offsets {
		if offset < 0 {
			out[i] = -1
			continue
		}
		out[i] = utf8.RuneCountInString(encoded[:offset])
	}
	return out
}
