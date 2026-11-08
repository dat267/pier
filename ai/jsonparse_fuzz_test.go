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

// FuzzStreamingArguments checks the incremental arguments parser never
// panics, every preview is valid JSON, and Finish equals the full parse for
// any split of the text.
func FuzzStreamingArguments(f *testing.F) {
	for _, seed := range []string{
		`{"a":1}`, `{"a":"xé😀"}`, `{"a" 1 "b":[1 2,]}`, `{"0":1,"b":2,"0":3}`,
		`{"a":"\ud83d`, `{"a":tru`, "{\"a\":\"\xf0\x9f\x98\"}", `[1]`, `{"a":1} x`, `{"a":1e`,
	} {
		f.Add(seed, uint8(3))
	}
	f.Fuzz(func(t *testing.T, input string, size uint8) {
		var args streamingArguments
		for rest := input; rest != ""; {
			n := min(len(rest), 1+int(size%16))
			if preview := args.Append(rest[:n]); !json.Valid(preview) {
				t.Fatalf("preview of %q = %q, not valid JSON", input, preview)
			}
			rest = rest[n:]
		}
		got, gotErr := args.Finish()
		want, wantErr := ParseToolCallArguments(input)
		if string(got) != string(want) || gotErr != wantErr {
			t.Fatalf("%q: Finish = %s %q, want %s %q", input, got, gotErr, want, wantErr)
		}
	})
}
