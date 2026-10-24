package ai

import (
	"encoding/json"
	"math"
	"testing"
)

// pi v1.4.0 API providers and session-manager.ts use JSON.stringify for
// numeric payloads too, including unpadded exponents and negative zero.
func TestMarshalJSONFormatsJavaScriptNumbers(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  string
	}{
		{math.Copysign(0, -1), `0`},
		{1e-7, `1e-7`},
		{json.RawMessage(`{"n":1.0,"small":1e-07,"zero":-0.0,"text":"-0 1e-07"}`), `{"n":1,"small":1e-7,"zero":0,"text":"-0 1e-07"}`},
	} {
		got, err := MarshalJSON(tt.value)
		if err != nil || string(got) != tt.want {
			t.Errorf("value=%v got=%s error=%v; want %s", tt.value, got, err, tt.want)
		}
		if _, ok := tt.value.(json.RawMessage); !ok {
			got, err = MarshalIndentJSON(tt.value)
			if err != nil || string(got) != tt.want {
				t.Errorf("indented value=%v got=%s error=%v; want %s", tt.value, got, err, tt.want)
			}
		}
	}
}

// D253: pi v1.4.0 core/session-manager.ts JSON.stringify uses JS doubles.
// Integer fields remain representable by the port's typed decoders; rounding
// MaxInt64 through float64 would serialize a number outside the int64 range.
func TestMarshalJSONPreservesTypedIntegerRange(t *testing.T) {
	value := struct {
		Number int64 `json:"number"`
	}{math.MaxInt64}
	encoded, err := MarshalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Number int64 `json:"number"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Number != value.Number {
		t.Fatalf("encoded=%s decoded=%d error=%v", encoded, decoded.Number, err)
	}
}

// Reference: pi v1.4.0 coding-agent/core/session-manager.ts persists with
// JSON.stringify; ai API implementations also stringify request payloads.
// Node's JSON.stringify leaves U+2028/U+2029 literal, unlike encoding/json.
func TestMarshalJSONPreservesJavaScriptLineSeparators(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{"separators", "a\u2028b\u2029c", "\"a\u2028b\u2029c\""},
		{"literal escapes", `literal \u2028 and \u2029`, `"literal \\u2028 and \\u2029"`},
		{"backslash before separator", "\\\u2028", "\"\\\\\u2028\""},
		{"html and controls", "<&>\n\t\u2029", "\"<&>\\n\\t\u2029\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, marshal := range []func(any) ([]byte, error){MarshalJSON, MarshalIndentJSON} {
				got, err := marshal(test.value)
				if err != nil || string(got) != test.want {
					t.Fatalf("got=%q err=%v want=%q", got, err, test.want)
				}
				var decoded string
				if err := json.Unmarshal(got, &decoded); err != nil || decoded != test.value {
					t.Fatalf("roundtrip=%q err=%v", decoded, err)
				}
			}
		})
	}
}

// JSON.stringify applies the same escaping to nested strings and property keys.
func TestMarshalJSONNestedLineSeparators(t *testing.T) {
	value := map[string]any{"key\u2028": []string{"\u2029", `\u2029`}}
	got, err := MarshalJSON(value)
	want := "{\"key\u2028\":[\"\u2029\",\"\\\\u2029\"]}"
	if err != nil || string(got) != want {
		t.Fatalf("got=%q err=%v want=%q", got, err, want)
	}
}
