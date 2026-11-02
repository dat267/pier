package highlight

import (
	"strings"
	"testing"
)

// pi v1.5.0 uses highlight.js 10.7.3 for JSON. Its repeated `0x` input
// highlights only the initial `0x0` as a number and emits the rest as text.
func TestLongInvalidJSONNumberMatchesReference(t *testing.T) {
	code := strings.Repeat("0x", 4096)
	result, err := HighlightWith(code, "json", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("root nodes = %d, want number and trailing text", len(result.Nodes))
	}
	number := result.Nodes[0]
	if number.Kind != "number" || len(number.Children) != 1 || number.Children[0].Text != "0x0" {
		t.Fatalf("number node = %#v", number)
	}
	if got, want := result.Nodes[1].Text, code[3:]; got != want {
		t.Fatalf("trailing text mismatch: len=%d, want len=%d", len(got), len(want))
	}
}
