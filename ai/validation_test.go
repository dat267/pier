package ai

import (
	"encoding/json"
	"testing"
)

// Upstream coerces a number to a string with String(value), so small
// magnitudes keep JS's exponent form (no zero-padded exponent).
func TestValidateToolArgumentsCoercesNumberToStringLikeJS(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)}
	cases := []struct {
		argument string
		want     string
	}{
		{`{"x":1e-7}`, `{"x":"1e-7"}`},
		{`{"x":1.5e-7}`, `{"x":"1.5e-7"}`},
		{`{"x":1e-6}`, `{"x":"0.000001"}`},
		{`{"x":1e16}`, `{"x":"10000000000000000"}`},
		{`{"x":1e21}`, `{"x":"1e+21"}`},
		{`{"x":42}`, `{"x":"42"}`},
		{`{"x":1.5}`, `{"x":"1.5"}`},
	}
	for _, testCase := range cases {
		call := ToolCall{ID: "c", Name: "t", Arguments: json.RawMessage(testCase.argument)}
		result, err := ValidateToolArguments(tool, call)
		if err != nil {
			t.Fatalf("ValidateToolArguments(%s): %v", testCase.argument, err)
		}
		if string(result) != testCase.want {
			t.Errorf("ValidateToolArguments(%s) = %s; want %s", testCase.argument, result, testCase.want)
		}
	}
}
