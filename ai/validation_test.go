package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// A finished call whose complete arguments text was not a JSON object fails
// validation with the raw text the model sent (#10944).
func TestValidateToolArgumentsReportsMalformedArguments(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
	args, errText := ParseToolCallArguments(`{"a":}`)
	call := ToolCall{ID: "c", Name: "t", Arguments: args, ArgumentsError: errText}
	_, err := ValidateToolArguments(tool, call)
	if err == nil {
		t.Fatal("malformed arguments were validated")
	}
	if !strings.Contains(err.Error(), `Invalid arguments for tool "t":`) ||
		!strings.Contains(err.Error(), "Received arguments:\n"+`{"a":}`) {
		t.Fatalf("error = %v", err)
	}
}

// validation.ts uses Number(value), which rejects trailing non-numeric text.
func TestValidateToolArgumentsRejectsNumericPrefixes(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`)}
	call := ToolCall{ID: "c", Name: "t", Arguments: json.RawMessage(`{"x":"1abc"}`)}
	if _, err := ValidateToolArguments(tool, call); err == nil {
		t.Fatal("numeric prefix must not coerce a string with trailing text")
	}
	call.Arguments = json.RawMessage(`{"x":"0x10"}`)
	result, err := ValidateToolArguments(tool, call)
	if err != nil || string(result) != `{"x":16}` {
		t.Fatalf("hex Number coercion = %s, %v; want 16", result, err)
	}
}

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
