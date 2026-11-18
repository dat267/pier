package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 packages/schema/src/compiler.ts stringGroup. A
// schema pattern constrains tool arguments; D317's TypeBox-only subset ignored
// it.
func TestValidateToolArgumentsEnforcesPattern(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","pattern":"^[a-z]+$"}},"required":["name"]}`)}
	_, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"name":"UPPER"}`)})
	if err == nil || !strings.Contains(err.Error(), "  - name: expected to match ^[a-z]+$") {
		t.Fatalf("error = %v; want pattern rejection", err)
	}
}

// pi v1.4.0 utils/validation.ts validates enum/const with TypeBox on decoded
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
		{"different type", `"enum":[1]`, `"2"`, true},
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

// pi v1.4.0 utils/validation.ts delegates combinator validation to TypeBox:
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

// pi v1.4.0 utils/validation.ts uses TypeBox's required and
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

// Reference: pi v1.4.0 (4a080252) packages/ai/src/utils/validation.ts and
// TypeBox 1.3.27 value/convert/from_integer.mjs. Type.Integer truncates
// numeric and numeric-string inputs before validation. Go carries the schema
// as JSON without TypeBox's runtime kind metadata.
func TestValidateToolArgumentsConvertsTypeBoxIntegers(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)}
	for _, args := range []string{`{"n":3.7}`, `{"n":"3.7"}`} {
		got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(args)})
		if err != nil || string(got) != `{"n":3}` {
			t.Errorf("ValidateToolArguments(%s) = %s, %v; want {\"n\":3}", args, got, err)
		}
	}
}

// Reference: pi v1.4.0 (4a080252) packages/ai/src/utils/validation.ts and
// TypeBox 1.3.27 value/convert/try/try_number.mjs.
// Value.Convert accepts case-insensitive boolean words as numbers.
func TestValidateToolArgumentsConvertsTypeBoxNumberWords(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"number"}},"required":["value"]}`)}
	for _, test := range []struct{ input, want string }{
		{`"TRUE"`, `1`},
		{`"False"`, `0`},
	} {
		got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"value":` + test.input + `}`)})
		if err != nil || string(got) != `{"value":`+test.want+`}` {
			t.Errorf("ValidateToolArguments(%s) = %s, %v; want %s", test.input, got, err, test.want)
		}
	}
}

// Reference: pi v1.4.0 (4a080252) packages/ai/src/utils/validation.ts and
// TypeBox 1.3.27 value/convert/try/try_boolean.mjs.
// Value.Convert accepts case-insensitive boolean strings and 0/1
// strings before validation.
func TestValidateToolArgumentsConvertsTypeBoxBooleanStrings(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"boolean"}},"required":["value"]}`)}
	for _, test := range []struct{ input, want string }{
		{`"True"`, `true`},
		{`"0"`, `false`},
		{`"1"`, `true`},
	} {
		got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"value":` + test.input + `}`)})
		if err != nil || string(got) != `{"value":`+test.want+`}` {
			t.Errorf("ValidateToolArguments(%s) = %s, %v; want %s", test.input, got, err, test.want)
		}
	}
}

// Reference: pi v1.4.0 (4a080252) packages/ai/src/utils/validation.ts and
// TypeBox 1.3.27 value/convert/try/try_string.mjs.
// Value.Convert converts required null string fields with
// String(null) before validation; optional nulls are handled separately.
func TestValidateToolArgumentsConvertsRequiredNullStringLikeTypeBox(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)}
	got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"command":null}`)})
	if err != nil || string(got) != `{"command":"null"}` {
		t.Fatalf("ValidateToolArguments(null string) = %s, %v; want {\"command\":\"null\"}", got, err)
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

// Reference: pi v1.4.0 utils/validation.ts uses TypeBox's finite number
// validation, but coercion to string still follows String(Infinity).
func TestValidateToolArgumentsHandlesOverflowedNumbers(t *testing.T) {
	for _, tc := range []struct {
		typ, want string
		invalid   bool
	}{
		// pi v1.5.2 (b4afdddf) matchesType uses Number.isFinite, so a number
		// schema no longer matches an overflowed argument.
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

// Reference: pi v1.5.0 f9a26365 packages/schema/src/compiler.ts
// annotatingParts and annotations.ts. An explicit true child evaluates a
// field/item even though it imposes no constraint, unlike an absent keyword.
func TestValidateToolArgumentsRetainsTrueAnnotations(t *testing.T) {
	for _, tc := range []struct{ name, schema string }{
		{"additional properties", `{"type":"object","additionalProperties":true,"unevaluatedProperties":false}`},
		{"unevaluated properties", `{"type":"object","allOf":[{"unevaluatedProperties":true}],"unevaluatedProperties":false}`},
		{"additional items", `{"type":"object","properties":{"a":{"type":"array","items":[true],"additionalItems":true,"unevaluatedItems":false}}}`},
		{"unevaluated items", `{"type":"object","properties":{"a":{"type":"array","allOf":[{"unevaluatedItems":true}],"unevaluatedItems":false}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validationOutcome(t, tc.schema, `{"a":[1,2]}`); got != `ok {"a":[1,2]}` {
				t.Fatal(got)
			}
		})
	}
	if got := validationOutcome(t, `{"type":"object","unevaluatedProperties":false}`, `{"a":1}`); got != "REJECT\n  - a: unknown field" {
		t.Fatalf("absent keyword control: %s", got)
	}
}

// Reference: pi v1.5.2 (b4afdddf) packages/ai/src/utils/validation.ts caches a
// compiled validator in a WeakMap keyed by the schema object, so a boolean
// schema (`true` or `false`) throws "Invalid value used as weak map key" before
// validation runs. D477: pier compiles the boolean schema instead, so `true`
// accepts every value and `false` rejects every value with the usual message.
func TestValidateToolArgumentsAcceptsBooleanRootSchemas(t *testing.T) {
	accepting := Tool{Name: "t", Parameters: json.RawMessage(`true`)}
	for _, args := range []string{`1`, `null`, `{}`, `[1]`} {
		got, err := ValidateToolArguments(accepting, ToolCall{Name: "t", Arguments: json.RawMessage(args)})
		if err != nil {
			t.Fatalf("true schema rejected %s: %v", args, err)
		}
		if string(got) != args {
			t.Fatalf("true schema returned %s for %s", got, args)
		}
	}
	rejecting := Tool{Name: "t", Parameters: json.RawMessage(`false`)}
	_, err := ValidateToolArguments(rejecting, ToolCall{Name: "t", Arguments: json.RawMessage(`1`)})
	if err == nil {
		t.Fatal("false schema accepted a value")
	}
	if !strings.Contains(err.Error(), "root: no value is allowed here") {
		t.Fatalf("err = %v, want the root rejection message", err)
	}
}

// Reference: pi v1.5.0 f9a26365 packages/schema/src/compiler.ts
// annotatingDynamicRef and explain.ts walk. Same-resource dynamic references
// evaluate fields/items just like $ref, before unevaluated checks run.
func TestValidateToolArgumentsTracksDynamicRefAnnotations(t *testing.T) {
	object := `{"type":"object","$dynamicRef":"#/$defs/O","$defs":{"O":{"properties":{"a":true}}},"unevaluatedProperties":false}`
	array := `{"type":"object","properties":{"a":{"type":"array","$dynamicRef":"#/$defs/A","unevaluatedItems":false}},"$defs":{"A":{"prefixItems":[true]}}}`
	for _, tc := range []struct{ schema, args, want string }{
		{object, `{"a":1}`, `ok {"a":1}`},
		{object, `{"a":1,"b":2}`, "REJECT\n  - b: unknown field"},
		{array, `{"a":[1]}`, `ok {"a":[1]}`},
		{array, `{"a":[1,2]}`, "REJECT\n  - a.1: unexpected item"},
		{`{"$dynamicRef":"#missing"}`, `{}`, "REJECT\n  - root: does not match the dynamically referenced schema"},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s\ngot %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 f9a26365 packages/schema/src/compiler.ts arrayGroup
// and coerce.ts convertArray. Empty tuple arrays are present tuples, not
// missing item schemas, and therefore never wrap a scalar into an array.
func TestValidateToolArgumentsHonorsEmptyTuples(t *testing.T) {
	closed := `{"type":"object","properties":{"a":{"type":"array","items":[],"additionalItems":false}}}`
	prefix := `{"type":"object","properties":{"a":{"type":"array","prefixItems":[]}}}`
	for _, tc := range []struct{ schema, args, want string }{
		{closed, `{"a":[1]}`, "REJECT\n  - a.0: no value is allowed here"},
		{closed, `{"a":"1"}`, "REJECT\n  - a: expected array, got string"},
		{closed, `{"a":[]}`, `ok {"a":[]}`},
		{prefix, `{"a":"1"}`, "REJECT\n  - a: expected array, got string"},
		{prefix, `{"a":[1]}`, `ok {"a":[1]}`},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s\ngot %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 f9a26365 packages/ai/src/utils/validation.ts
// formatValidationPath and packages/schema/src/explain.ts. These literals were
// checked against validateToolArguments from that revision.
func TestValidationErrorPrintsParsedOriginalArguments(t *testing.T) {
	_, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`)}, ToolCall{Name: "t", Arguments: json.RawMessage(`{"n":"\u0074wo"}`)})
	want := "Validation failed for tool \"t\":\n  - n: expected integer, got string\n\nReceived arguments:\n{\n  \"n\": \"two\"\n}"
	if err == nil || err.Error() != want {
		t.Fatalf("error=%v; want %s", err, want)
	}
}

func TestValidationErrorsPointToFailingArgument(t *testing.T) {
	for _, tc := range []struct{ schema, args, lines string }{
		{`{"type":"object","properties":{"n":{"type":["integer"]}}}`, `{"n":"bad"}`, "  - n: expected integer, got string"},
		{`{"type":"object","properties":{"n":{"const":1}}}`, `{"n":2}`, "  - n: expected 1"},
		{`{"type":"object","properties":{"n":{"enum":[1]}}}`, `{"n":2}`, "  - n: expected one of [1]"},
		{`{"type":"object","properties":{"n":false}}`, `{"n":2}`, "  - n: no value is allowed here"},
		{`{"type":"object","additionalProperties":false}`, `{"extra":2}`, "  - extra: unknown field"},
		{`{"type":"object","properties":{"n":{"anyOf":[{"type":"integer"},{"type":"null"}]}}}`, `{"n":"bad"}`, "  - n: expected integer, got string"},
		{`{"type":"object","properties":{"n":{"oneOf":[{"type":"number"},{"type":"integer"}]}}}`, `{"n":1}`, "  - n: matches more than one alternative"},
		{`{"type":"object","properties":{"n":{"allOf":[{"type":"integer"},{"const":1}]}}}`, `{"n":"bad"}`, "  - n: expected integer, got string\n  - n: expected 1"},
		{`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`, `{"count":"two"}`, "  - count: expected integer, got string"},
		{`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`, `{}`, "  - count: missing"},
		{`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"count":{"type":"integer"}}}}}}`, `{"items":[{"count":"two"}]}`, "  - items.0.count: expected integer, got string"},
		{`{"type":"object","properties":{"name":{"type":"string","pattern":"^[a-z]+$"}}}`, `{"name":"UPPER"}`, "  - name: expected to match ^[a-z]+$"},
	} {
		_, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(tc.schema)}, ToolCall{Name: "t", Arguments: json.RawMessage(tc.args)})
		if err == nil {
			t.Fatal("expected validation error")
		}
		parts := strings.SplitN(err.Error(), "\n\nReceived arguments:", 2)
		want := "Validation failed for tool \"t\":\n" + tc.lines
		if parts[0] != want {
			t.Errorf("args=%s\ngot %s\nwant %s", tc.args, parts[0], want)
		}
	}
}

// Reference: pi v1.5.0 f9a26365 packages/schema/src/checks.ts isMultiple
// scales decimal coefficients with BigInt, so a tiny non-multiple is rejected
// without overflowing its divisor or panicking.
func TestValidateToolArgumentsRejectsTinyNonMultiple(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"number","multipleOf":3}},"required":["n"]}`)}
	_, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"n":1e-100}`)})
	if err == nil || !strings.Contains(err.Error(), "n: expected a multiple of 3") {
		t.Fatalf("tiny non-multiple error = %v", err)
	}
	got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"n":6}`)})
	if err != nil || string(got) != `{"n":6}` {
		t.Fatalf("valid multiple = %s, %v", got, err)
	}
}

// Reference: pi v1.5.0 f9a26365 packages/schema/src/coerce.ts dropOptionalNulls.
// additionalProperties never applies to a key matched by patternProperties,
// even in the optional-null cleanup pass.
func TestValidateToolArgumentsPreservesPatternMatchedNulls(t *testing.T) {
	schema := `{"type":"object","patternProperties":{"^a$":{}},"additionalProperties":{"type":"object","properties":{"n":{"type":"integer"}}}}`
	args := `{"a":{"n":null},"b":{"n":null}}`
	want := `ok {"a":{"n":null},"b":{}}`
	if got := validationOutcome(t, schema, args); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// D364: pi v1.5.0 (f9a26365) packages/schema/src/checks.ts compiles `pattern`
// as an ECMAScript RegExp with the `u` flag. Go's RE2 has no lookbehind and no
// backreferences, so a valid ECMAScript pattern using them leaves the string
// unconstrained, where the reference would reject the value below. A pattern RE2
// does support still constrains (the control), and a pattern that is malformed
// in ECMAScript too still fails compilation
// (TestValidateToolArgumentsCompilesPiSchema).
func TestValidateToolArgumentsLeavesRE2UnsupportedPatternsUnconstrained(t *testing.T) {
	validate := func(pattern string) error {
		schema, _ := json.Marshal(map[string]any{
			"type":       "object",
			"properties": map[string]any{"s": map[string]any{"type": "string", "pattern": pattern}},
		})
		_, err := ValidateToolArguments(Tool{Name: "t", Parameters: schema}, ToolCall{Name: "t", Arguments: json.RawMessage(`{"s":"zzz"}`)})
		return err
	}
	if err := validate(`^a+$`); err == nil {
		t.Fatal("control: a supported pattern accepted a non-matching value")
	}
	for _, pattern := range []string{`(?<=a)b`, `(?<!z)a`, `^(a)\1$`, `^(?<x>a)\k<x>$`} {
		if err := validate(pattern); err != nil {
			t.Errorf("pattern %q: %v; want it left unconstrained", pattern, err)
		}
	}
}

// pi v1.5.2 (b4afdddf), packages/schema/src/coerce.ts convertUnion: a branch
// probe goes through passes(), which returns undefined when the check throws, and
// a self-referential branch makes it throw. So the recursive branch does not count
// as passing, and the union tries the next branch and converts the value.
func TestSelfReferentialUnionCoerces(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"loop": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"$ref": "#/$defs/loop"},
				},
			},
		},
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"$ref": "#/$defs/loop"},
		},
	}
	parameters, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := json.Marshal(map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	tool := Tool{Name: "self-ref", Parameters: parameters}
	encoded, err := ValidateToolArguments(tool, ToolCall{Name: "self-ref", Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	value, ok := got["a"].(string)
	if !ok || value != "1" {
		t.Fatalf("a = %#v (arguments %s), want the string \"1\" the union converts to", got["a"], encoded)
	}
}

// Reference: pi v1.5.0 f9a26365 packages/schema/src/coerce.ts itemSchemaAt.
// A legacy tuple's additionalItems is checked, but none of the coercion
// passes converts its values or removes optional nulls inside them.
func TestValidateToolArgumentsLeavesAdditionalTupleItemsUnchanged(t *testing.T) {
	for _, tc := range []struct{ schema, args, want string }{
		{`{"type":"object","properties":{"a":{"type":"array","items":[true],"additionalItems":{"type":"number"}}}}`, `{"a":[0,"1"]}`, "REJECT\n  - a.1: expected number, got string"},
		{`{"type":"object","properties":{"a":{"type":"array","items":[true],"additionalItems":{"type":"object","properties":{"n":{"type":"integer"}}}}}}`, `{"a":[0,{"n":null}]}`, "REJECT\n  - a.1.n: expected integer, got null"},
		{`{"type":"object","properties":{"a":{"type":"array","prefixItems":[true],"items":{"type":"number"}}}}`, `{"a":[0,"1"]}`, `ok {"a":[0,1]}`},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("args=%s\ngot %s\nwant %s", tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 f9a26365 packages/schema/src/coerce.ts convertArray
// and reenter. A wrapped item is converted once, including const/enum rules.
// Repeated primitive values still cut off recursive conversions.
func TestValidateToolArgumentsConvertsWrappedItems(t *testing.T) {
	literal := `{"type":"object","properties":{"a":{"type":"array","items":{"const":1}}}}`
	recursive := `{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}},"$defs":{"A":{"anyOf":[{"type":"array","items":{"$ref":"#/$defs/A"}}]}}}`
	for _, tc := range []struct{ schema, args, want string }{
		{literal, `{"a":"1"}`, `ok {"a":[1]}`},
		{literal, `{"a":["1"]}`, `ok {"a":[1]}`},
		{recursive, `{"a":1}`, "REJECT\n  - a: expected array, got integer"},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s\ngot %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.2 (b4afdddf) packages/schema/src/coerce.ts pass 3
// (coercePrimitive). It runs only for a type the value does not already match,
// and unlike pass 2 (convertNode) it never truncates an existing number for
// "integer", never reads "true"/"false" as a number, and turns null into ""
// for "string" (pass 2 turns it into "null"). The two passes are separate in
// the reference, so a union type array takes pass 3 alone.
func TestValidateToolArgumentsCoercesUnionsLikePiSchemaPass3(t *testing.T) {
	cases := []struct {
		name     string
		schema   string
		args     string
		want     string // "" means the call must fail validation
		wantFail bool
	}{
		{"integer union keeps the number", `{"type":"object","properties":{"v":{"type":["integer","null"]}}}`,
			`{"v":3.7}`, "", true},
		{"integer union keeps the numeric string", `{"type":"object","properties":{"v":{"type":["integer","null"]}}}`,
			`{"v":"3.7"}`, "", true},
		{"integer union converts a non-number to the string branch", `{"type":"object","properties":{"v":{"type":["integer","string"]}}}`,
			`{"v":3.7}`, `{"v":"3.7"}`, false},
		{"number union reads a word as the boolean branch", `{"type":"object","properties":{"v":{"type":["number","boolean"]}}}`,
			`{"v":"true"}`, `{"v":true}`, false},
		{"number union reads false as the boolean branch", `{"type":"object","properties":{"v":{"type":["number","boolean"]}}}`,
			`{"v":"false"}`, `{"v":false}`, false},
		{"number union does not read a word as a number", `{"type":"object","properties":{"v":{"type":["number","null"]}}}`,
			`{"v":"true"}`, "", true},
		{"string union turns a required null into the empty string", `{"type":["string","number"]}`,
			`null`, `""`, false},
		{"integer union reads true as the boolean branch", `{"type":"object","properties":{"v":{"type":["integer","boolean"]}}}`,
			`{"v":"true"}`, `{"v":true}`, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := Tool{Name: "t", Parameters: json.RawMessage(testCase.schema)}
			got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(testCase.args)})
			if testCase.wantFail {
				if err == nil {
					t.Fatalf("%s = %s, want a validation error", testCase.args, got)
				}
				if !strings.Contains(err.Error(), "Validation failed") {
					t.Fatalf("err = %v, want a validation failure", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", testCase.args, err)
			}
			if string(got) != testCase.want {
				t.Fatalf("%s = %s, want %s", testCase.args, got, testCase.want)
			}
		})
	}
}
