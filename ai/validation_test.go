package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// pi v1.3.0 utils/validation.ts validates enum/const with TypeBox on decoded
// values, independent of JSON whitespace, number spelling and member order.
func TestValidateToolArgumentsMatchesLiteralValues(t *testing.T) {
	for _, tt := range []struct {
		name, constraint, value string
		invalid                 bool
	}{
		{"number spelling", `"const":1.0`, `1`, false},
		{"negative zero", `"const":-0`, `0`, false},
		{"escaped string", `"enum":["\u0061"]`, `"a"`, false},
		{"object order", `"const":{"a":1,"b":2}`, `{"b":2,"a":1}`, false},
		{"nested enum", `"enum":[{"a":[1,2],"b":true}]`, `{"b":true,"a":[1,2]}`, false},
		{"different array", `"const":[1,2]`, `[2,1]`, true},
		{"different object", `"const":{"a":1}`, `{"a":1,"b":2}`, true},
		{"different type", `"enum":[1]`, `"1"`, true},
		{"empty enum", `"enum":[]`, `1`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{` + tt.constraint + `}},"required":["x"]}`)}
			out, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"x":` + tt.value + `}`)})
			if (err != nil) != tt.invalid {
				t.Fatalf("result=%s error=%v; invalid=%v", out, err, tt.invalid)
			}
		})
	}
}

// pi v1.3.0 utils/validation.ts delegates combinator validation to TypeBox:
// oneOf requires exactly one match and anyOf/oneOf constraints both apply.
func TestValidateToolArgumentsChecksCombinatorsIndependently(t *testing.T) {
	for _, tt := range []struct {
		schema, value string
		invalid       bool
	}{
		{`{"oneOf":[{},{}]}`, `2`, true},
		{`{"oneOf":[{"type":"number"},{"type":"integer"}]}`, `1`, true},
		{`{"oneOf":[{"type":"number"},{"type":"integer"}]}`, `1.5`, false},
		{`{"anyOf":[{"type":"string"}],"oneOf":[{"type":"number"}]}`, `"x"`, true},
		{`{"anyOf":[]}`, `2`, true},
		{`{"oneOf":[]}`, `2`, true},
		{`{"allOf":[]}`, `2`, false},
	} {
		tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"x":` + tt.schema + `},"required":["x"]}`)}
		out, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"x":` + tt.value + `}`)})
		if (err != nil) != tt.invalid {
			t.Errorf("schema=%s value=%s result=%s error=%v; invalid=%v", tt.schema, tt.value, out, err, tt.invalid)
		}
	}
}

// pi v1.3.0 utils/validation.ts uses TypeBox's required and
// additionalProperties checks even when a required key has no property schema.
func TestValidateToolArgumentsEnforcesObjectConstraints(t *testing.T) {
	for _, tt := range []struct {
		schema, value string
		invalid       bool
	}{
		{`{"type":"object","properties":{"x":{"type":"number"}},"additionalProperties":false}`, `{"x":1,"extra":2}`, true},
		{`{"type":"object","properties":{"x":{"type":"number"}},"additionalProperties":false}`, `{"x":1}`, false},
		{`{"type":"object","required":["x"]}`, `{}`, true},
		{`{"type":"object","required":["x"]}`, `{"x":null}`, false},
		{`{"type":"object","properties":{},"additionalProperties":false}`, `{"x":1}`, true},
		{`{"type":"object","properties":{"x":false}}`, `{"x":1}`, true},
		{`{"type":"object","properties":{"x":false}}`, `{}`, false},
		{`{"type":"object","properties":{"x":true}}`, `{"x":1}`, false},
	} {
		out, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(tt.schema)}, ToolCall{Name: "t", Arguments: json.RawMessage(tt.value)})
		if (err != nil) != tt.invalid {
			t.Errorf("schema=%s arguments=%s result=%s error=%v; invalid=%v", tt.schema, tt.value, out, err, tt.invalid)
		}
	}
}

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

// Reference: pi v1.3.0 utils/validation.ts uses TypeBox's finite number
// validation, but coercion to string still follows String(Infinity).
func TestValidateToolArgumentsHandlesOverflowedNumbers(t *testing.T) {
	for _, tc := range []struct {
		typ, want string
		invalid   bool
	}{
		{typ: "number", invalid: true}, {typ: "integer", invalid: true}, {typ: "string", want: `{"x":"Infinity"}`}, {want: `{"x":null}`},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			schema := `{"type":"object","properties":{}}`
			if tc.typ != "" {
				schema = `{"type":"object","properties":{"x":{"type":"` + tc.typ + `"}}}`
			}
			out, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(schema)}, ToolCall{Name: "t", Arguments: json.RawMessage(`{"x":1e400}`)})
			if tc.invalid {
				if err == nil {
					t.Fatalf("nonfinite %s accepted: %s", tc.typ, out)
				}
				return
			}
			if err != nil || string(out) != tc.want {
				t.Fatalf("args=%s err=%v want=%s", out, err, tc.want)
			}
		})
	}
}

// utils/validation.ts uses Number(value); a radix prefix cannot be followed
// by a sign, even though math/big.Int.SetString accepts signed substrings.
func TestJSNumberStringRejectsSignsAfterRadixPrefix(t *testing.T) {
	for _, value := range []string{"0x-1", "0x+1", "0b-1", "0o+7"} {
		if number, valid := ParseJSNumberString(value); valid {
			t.Errorf("%q accepted as %v", value, number)
		}
	}
}
