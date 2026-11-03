package ai

import "testing"

// Upstream parses these fragments with JSON.parse, which rejects trailing
// data after the object (D359).
func TestDecodeOrderedJSONFieldsRejectsTrailing(t *testing.T) {
	for _, input := range []string{`{"a":1} trailing`, `{"a":1}{"b":2}`, `{"a":1}]`, `{"a":1`, `{"a":1,"b":2`} {
		if _, err := decodeOrderedJSONFields([]byte(input)); err == nil {
			t.Errorf("decodeOrderedJSONFields(%q) accepted trailing data", input)
		}
	}
	if fields, err := decodeOrderedJSONFields([]byte(" {\"a\":1}\n")); err != nil || fields == nil {
		t.Fatalf("trailing whitespace = %v/%v", fields, err)
	}
}
