package ai

import (
	"encoding/json"
	"testing"
)

// FuzzJSONRepair checks the streaming JSON repair paths never panic and always
// produce parseable output for a Go value.
func FuzzJSONRepair(f *testing.F) {
	seeds := []string{
		`{}`, `{"a":1}`, `{"a":`, `{"a":"unterminated`, `[1,2,`,
		`{"a":1,"b":[2,3}`, `{`, ``, `null`, `true`, `{"a":1} trailing`,
		`"\u00`, `[{"x":`, `{"a": 1, "b": 2, "c": `, `}`, `{{{`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_ = RepairJSON(input)
		var value any
		if err := ParseJSONWithRepair(input, &value); err == nil {
			// A successful parse must re-marshal.
			if _, err := MarshalJSON(value); err != nil {
				t.Fatalf("a repaired value did not marshal: %q: %v", input, err)
			}
		}
		raw := ParseStreamingJSONText(input)
		if !json.Valid(raw) {
			t.Fatalf("ParseStreamingJSONText(%q) = %q, not valid JSON", input, raw)
		}
	})
}
