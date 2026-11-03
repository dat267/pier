package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 packages/ai/src/utils/validation.ts and
// packages/schema/src/explain.ts. Validation failures list pi-schema's
// messages, not TypeBox's.
func TestValidateToolArgumentsUsesPiSchemaErrorMessages(t *testing.T) {
	tool := Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"number"}},"required":["value"]}`)}
	_, err := ValidateToolArguments(tool, ToolCall{Name: "echo", Arguments: json.RawMessage(`{"value":"many"}`)})
	if err == nil {
		t.Fatal("expected validation error")
	}
	want := "Validation failed for tool \"echo\":\n  - value: expected number, got string\n\nReceived arguments:\n{\n  \"value\": \"many\"\n}"
	if err.Error() != want {
		t.Fatalf("error =\n%s\nwant:\n%s", err, want)
	}
}

func mustValidate(t *testing.T, schema, args string) string {
	t.Helper()
	got, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(schema)}, ToolCall{Name: "t", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("ValidateToolArguments(%s, %s): %v", schema, args, err)
	}
	return string(got)
}

// Reference: pi v1.5.0 packages/ai/test/validation.test.ts and
// packages/ai/test/validation-probes.test.ts. pi-schema wraps a single value
// into a one-item array and converts enum/const members before checking.
func TestValidateToolArgumentsCoercesPiSchemaValues(t *testing.T) {
	for _, tc := range []struct{ schema, args, want string }{
		{`{"type":"object","properties":{"value":{"type":"array","items":{"type":"number"}}}}`, `{"value":"7"}`, `{"value":[7]}`},
		{`{"type":"object","properties":{"value":{"type":"integer"}}}`, `{"value":"42.1"}`, `{"value":42}`},
		{`{"type":"object","properties":{"value":{"const":1}}}`, `{"value":"1"}`, `{"value":1}`},
		{`{"type":"object","properties":{"value":{"enum":[1,"x"]}}}`, `{"value":"1"}`, `{"value":1}`},
		{`{"type":"object","properties":{"value":{"type":["boolean","number"]}}}`, `{"value":"1"}`, `{"value":1}`},
	} {
		if got := mustValidate(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s got %s want %s", tc.schema, tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 packages/schema/src/coerce.ts dropOptionalNulls. The
// optional-null pass crosses patternProperties and additionalProperties, not
// only declared properties.
func TestValidateToolArgumentsDropsOptionalNullsNested(t *testing.T) {
	item := `{"type":"object","properties":{"n":{"type":"integer","minimum":1}}}`
	for _, schema := range []string{
		`{"type":"object","properties":{"entry":` + item + `}}`,
		`{"type":"object","additionalProperties":` + item + `}`,
		`{"type":"object","patternProperties":{"^entry$":` + item + `},"additionalProperties":false}`,
	} {
		if got := mustValidate(t, schema, `{"entry":{"n":null}}`); got != `{"entry":{}}` {
			t.Errorf("schema=%s got %s", schema, got)
		}
	}
}

// Reference: pi v1.5.0 packages/schema/src/coerce.ts and
// packages/ai/test/validation.test.ts. Local $refs participate in null
// dropping and coercion; a dangling ref rejects only values at its place.
func TestValidateToolArgumentsResolvesLocalRefs(t *testing.T) {
	nullable := `{"type":"object","properties":{"value":{"$ref":"#/$defs/value"}},"$defs":{"value":{"anyOf":[{"type":"number"},{"type":"null"}]}}}`
	if got := mustValidate(t, nullable, `{"value":null}`); got != `{"value":null}` {
		t.Errorf("nullable ref got %s", got)
	}
	required := `{"type":"object","properties":{"value":{"$ref":"#/$defs/value"}},"required":["value"],"$defs":{"value":{"type":"string"}}}`
	if got := mustValidate(t, required, `{"value":null}`); got != `{"value":"null"}` {
		t.Errorf("required ref got %s", got)
	}
	dangling := `{"type":"object","properties":{"t":{"$ref":"#/definitions/Missing"}}}`
	_, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(dangling)}, ToolCall{Name: "t", Arguments: json.RawMessage(`{"t":1}`)})
	if err == nil || !strings.Contains(err.Error(), "  - t: unresolved reference #/definitions/Missing") {
		t.Fatalf("dangling ref error = %v", err)
	}
	if got := mustValidate(t, dangling, `{}`); got != `{}` {
		t.Errorf("dangling ref absent got %s", got)
	}
}

func validationOutcome(t *testing.T, schema, args string) string {
	t.Helper()
	got, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(schema)}, ToolCall{Name: "t", Arguments: json.RawMessage(args)})
	if err != nil {
		parts := strings.Split(err.Error(), "\n")
		if len(parts) > 1 {
			return "REJECT\n" + parts[1]
		}
		return "REJECT"
	}
	return "ok " + string(got)
}

// Reference: pi v1.5.0 packages/ai/test/validation-probes.test.ts. These are
// plain-JSON (MCP-shaped) probes; TypeBox-only constructs are omitted.
func TestValidateToolArgumentsMatchesPiSchemaProbes(t *testing.T) {
	type probe struct{ schema, args, want string }
	probes := []probe{
		{`{"type":"object","properties":{"kind":{"type":"string"},"n":{"type":"number"}},"if":{"properties":{"kind":{"const":"a"}}},"then":{"required":["n"]}}`, `{"kind":"a"}`, "REJECT\n  - n: missing"},
		{`{"type":"object","properties":{"kind":{"type":"string"},"n":{"type":"number"}},"if":{"properties":{"kind":{"const":"a"}}},"then":{"required":["n"]}}`, `{"kind":"b"}`, `ok {"kind":"b"}`},
		{`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"dependentRequired":{"a":["b"]}}`, `{"a":"x"}`, "REJECT\n  - b: required when a is present"},
		{`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"dependencies":{"a":["b"]}}`, `{"a":"x"}`, "REJECT\n  - b: required when a is present"},
		{`{"type":"object","propertyNames":{"pattern":"^[a-z]+$"}}`, `{"abc":1}`, `ok {"abc":1}`},
		{`{"type":"object","propertyNames":{"pattern":"^[a-z]+$"}}`, `{"ABC":1}`, "REJECT\n  - ABC: invalid property name"},
		{`{"type":"object","properties":{"l":{"type":"array","contains":{"type":"number"}}}}`, `{"l":["a"]}`, "REJECT\n  - l: expected at least 1 matching item"},
		{`{"type":"object","properties":{"l":{"type":"array","contains":{"type":"number"}}}}`, `{"l":["a",1]}`, `ok {"l":["a",1]}`},
		{`{"type":"object","properties":{"s":{"not":{"type":"null"}}}}`, `{"s":"x"}`, `ok {"s":"x"}`},
		{`{"type":"object","properties":{"s":{"not":{"type":"null"}}}}`, `{"s":null}`, `ok {}`},
		{`{"type":"object","properties":{"m":{"type":"object","minProperties":1}}}`, `{"m":{}}`, "REJECT\n  - m: expected at least 1 properties"},
		{`{"type":"object","properties":{"a":{"type":"string"}},"unevaluatedProperties":false}`, `{"a":"x"}`, `ok {"a":"x"}`},
		{`{"type":"object","properties":{"a":{"type":"string"}},"unevaluatedProperties":false}`, `{"a":"x","b":1}`, "REJECT\n  - b: unknown field"},
		{`{"allOf":[{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false},{"type":"object","properties":{"b":{"type":"string"}}}]}`, `{"a":"x"}`, `ok {"a":"x"}`},
		{`{"allOf":[{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false},{"type":"object","properties":{"b":{"type":"string"}}}]}`, `{"a":"x","b":"y"}`, "REJECT\n  - b: unknown field"},
		{`{"type":"object","properties":{"c":{"const":{"x":1,"y":2}}}}`, `{"c":{"y":2,"x":1}}`, `ok {"c":{"y":2,"x":1}}`},
		{`{"type":"object","properties":{"v":{"type":["string","number"]}}}`, `{"v":true}`, `ok {"v":"true"}`},
		{`{"type":"object","properties":{"v":{"type":["string","number"]}}}`, `{"v":null}`, `ok {}`},
		{`{"type":"object","properties":{"v":{"type":["string","null"]}},"required":["v"]}`, `{"v":null}`, `ok {"v":null}`},
		{`{"type":"object","properties":{"v":{"type":["string","null"]}},"required":["v"]}`, `{"v":5}`, `ok {"v":"5"}`},
		{`{"type":"object","properties":{"n":{"type":"number","exclusiveMinimum":0}}}`, `{"n":0}`, "REJECT\n  - n: expected > 0"},
		{`{"type":"object","properties":{"n":{"type":"number","exclusiveMinimum":0}}}`, `{"n":"0"}`, "REJECT\n  - n: expected > 0"},
		{`{"type":"object","properties":{"l":{"type":"array","maxItems":1}}}`, `{"l":[1,2]}`, "REJECT\n  - l: expected at most 1 items"},
		{`{"type":"object","properties":{"l":{"type":"array","maxItems":1}}}`, `{"l":"x"}`, `ok {"l":["x"]}`},
		{`{"type":"object","properties":{"p":{"oneOf":[{"type":"object","properties":{"kind":{"type":"string","enum":["a"]},"x":{"type":"number"}},"required":["kind"]},{"type":"object","properties":{"kind":{"type":"string","enum":["b"]}},"required":["kind"]}],"discriminator":{"propertyName":"kind"}}}}`, `{"p":{"kind":"a","x":"1"}}`, `ok {"p":{"kind":"a","x":1}}`},
		{`{"type":"object","properties":{"p":{"oneOf":[{"type":"object","properties":{"kind":{"type":"string","enum":["a"]},"x":{"type":"number"}},"required":["kind"]},{"type":"object","properties":{"kind":{"type":"string","enum":["b"]}},"required":["kind"]}],"discriminator":{"propertyName":"kind"}}}}`, `{"p":{"kind":"b"}}`, `ok {"p":{"kind":"b"}}`},
		{`{"$defs":{"Root":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}},"$ref":"#/$defs/Root"}`, `{"a":"x"}`, `ok {"a":"x"}`},
		{`{"$defs":{"Root":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}},"$ref":"#/$defs/Root"}`, `{"a":1}`, `ok {"a":"1"}`},
		{`{"$defs":{"Root":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}},"$ref":"#/$defs/Root"}`, `{}`, "REJECT\n  - a: missing"},
		{`{"type":"object","properties":{"a":{"$ref":"#/$defs/a%20b"}},"$defs":{"a b":{"type":"string"}}}`, `{"a":1}`, `ok {"a":"1"}`},
		{`{"type":"object","properties":{"a":{"$ref":"#/$defs/a~1b"}},"$defs":{"a/b":{"type":"string"}}}`, `{"a":"x"}`, `ok {"a":"x"}`},
		{`{"type":"object","properties":{"a":{"$ref":"item.json"}},"$defs":{"item":{"$id":"item.json","type":"string"}}}`, `{"a":2}`, `ok {"a":"2"}`},
		{`{"type":"object","properties":{"v":{"anyOf":[{"type":"string"},{"type":"null"}],"default":null}}}`, `{"v":null}`, `ok {"v":null}`},
		{`{"type":"object","properties":{"d":{"type":"string","format":"date-time"}}}`, `{"d":"2024-01-01t00:00:00z"}`, `ok {"d":"2024-01-01t00:00:00z"}`},
		{`{"type":"object","properties":{"d":{"type":"string","format":"date-time"}}}`, `{"d":"2024-01-01 00:00:00Z"}`, "REJECT\n  - d: expected format date-time"},
		{`{"type":"object","properties":{"u":{"type":"string","format":"url"}}}`, `{"u":"not a url"}`, "REJECT\n  - u: expected format url"},
		{`{"type":"object","properties":{"n":{"type":"number"}}}`, `{"n":" 12 "}`, `ok {"n":12}`},
		{`{"type":"object","properties":{"n":{"type":"number"}}}`, `{"n":"0x10"}`, `ok {"n":16}`},
		{`{"type":"object","properties":{"n":{"type":"number"}}}`, `{"n":""}`, `ok {"n":0}`},
		{`{"type":"object","properties":{"p":{"type":"array","items":[{"type":"number"},{"type":"string"}],"additionalItems":false}}}`, `{"p":[1,"a"]}`, `ok {"p":[1,"a"]}`},
		{`{"type":"object","properties":{"p":{"type":"array","items":[{"type":"number"},{"type":"string"}],"additionalItems":false}}}`, `{"p":[1,"a",3]}`, "REJECT\n  - p.2: no value is allowed here"},
		{`{"type":"object","properties":{"p":{"type":"array","items":[{"type":"number"},{"type":"string"}],"additionalItems":false}}}`, `{"p":["1",2]}`, `ok {"p":[1,"2"]}`},

		{`{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"type":"string","maxLength":5}]}}}`, `{"v":"abc"}`, "REJECT\n  - v: matches more than one alternative"},
		{`{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"type":"string","maxLength":5}]}}}`, `{"v":"abcdefgh"}`, `ok {"v":"abcdefgh"}`},
		{`{"type":"object","properties":{"v":{"oneOf":[{"type":"number"},{"type":"integer"}]}}}`, `{"v":2}`, "REJECT\n  - v: matches more than one alternative"},
		{`{"type":"object","properties":{"v":{"oneOf":[{"type":"number"},{"type":"integer"}]}}}`, `{"v":2.5}`, `ok {"v":2.5}`},
		{`{"type":"object","properties":{"v":{"oneOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"string"}}}]}}}`, `{"v":{"a":"x"}}`, "REJECT\n  - v: matches more than one alternative"},
		{`{"type":"object","properties":{"v":{"oneOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"string"}}}]}}}`, `{"v":{}}`, "REJECT\n  - v: matches more than one alternative"},
		{`{"type":"object","properties":{"s":{"type":"string","nullable":true}},"required":["s"]}`, `{"s":null}`, `ok {"s":"null"}`},
		{`{"type":"object","properties":{"t":{"$ref":"#/definitions/T"}},"required":["t"],"definitions":{"T":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}`, `{"t":{"path":42}}`, `ok {"t":{"path":"42"}}`},
		{`{"type":"object","properties":{"s":{"$ref":"#/definitions/S","maxLength":1}},"definitions":{"S":{"type":"string"}}}`, `{"s":"abc"}`, "REJECT\n  - s: expected at most 1 characters"},
		{`{"type":"object","properties":{"n":{"type":"number","minimum":0,"exclusiveMinimum":true}}}`, `{"n":0}`, `ok {"n":0}`},
		{`{"type":"object","properties":{"a":{"type":"string"}},"id":"http://x/y"}`, `{"a":"x"}`, `ok {"a":"x"}`},
		{`{"type":"object","properties":{"s":{"type":"string","pattern":"(?<!x)y"}}}`, `{"s":"y"}`, `ok {"s":"y"}`},
		{`{"type":"object","properties":{"n":{"type":"integer"}}}`, `{"n":1.5}`, `ok {"n":1}`},
		{`{"type":"object","properties":{"n":{"type":"integer"}}}`, `{"n":3.7}`, `ok {"n":3}`},
		{`{"type":"object","properties":{"n":{"type":"integer"}}}`, `{"n":"3.7"}`, `ok {"n":3}`},
		{`{"type":"object","properties":{"a":true,"b":false}}`, `{"b":1}`, "REJECT\n  - b: no value is allowed here"},
		{`{"type":"object","properties":{"a":true,"b":false}}`, `{"a":1}`, `ok {"a":1}`},
		{`{"type":"object","properties":{"e":{"type":"string","enum":["v0","v1","v2","v3","v4","v5","v6","v7","v8","v9","v10","v11","v12","v13","v14","v15","v16","v17","v18","v19","v20"]}}}`, `{"e":"v21"}`, "REJECT\n  - e: expected one of [\"v0\",\"v1\",\"v2\",\"v3\",\"v4\",\"v5\",\"v6\",\"v7\",\"v8\",\"v9\",\"v10\",\"v11\",\"v12\",\"v13\",\"v14\",\"v15\",\"v16\",\"v17\",\"v18\",\"v19\", ... and 1 more]"},
		{`{"type":"object","properties":{"l":{"type":"array","contains":{"type":"number"},"minContains":2}}}`, `{"l":["a",1]}`, "REJECT\n  - l: expected at least 2 matching items"},
		{`{"type":"object","properties":{"l":{"type":"array","contains":{"type":"number"},"maxContains":1}}}`, `{"l":[1,2]}`, "REJECT\n  - l: expected between 1 and 1 matching items"},
		{`{"type":"object","properties":{"l":{"type":"array","uniqueItems":true}}}`, `{"l":[{"x":1,"y":2},{"y":2,"x":1}]}`, "REJECT\n  - l: expected unique items"},
		{`{"type":"object","properties":{"s":{"type":"string","minLength":2}}}`, `{"s":"😀"}`, "REJECT\n  - s: expected at least 2 characters"},
		{`{"type":"object","properties":{"s":{"type":"string","maxLength":1}}}`, `{"s":"😀"}`, `ok {"s":"😀"}`},
		{`{"type":"object","properties":{"n":{"type":"number","multipleOf":0.01}}}`, `{"n":0.07}`, `ok {"n":0.07}`},
		{`{"type":"object","properties":{"n":{"type":"number","multipleOf":0.01}}}`, `{"n":1.1}`, `ok {"n":1.1}`},
		{`{"type":"object","properties":{"s":{"type":"string","format":"email"}}}`, `{"s":"not-an-email"}`, "REJECT\n  - s: expected format email"},
		{`{"type":"object","properties":{"s":{"type":"string","format":"uri"}}}`, `{"s":"https://x.y/z"}`, `ok {"s":"https://x.y/z"}`},
		{`{"type":"object","properties":{"h":{"type":"string","format":"hostname"}}}`, `{"h":"under_score.example"}`, "REJECT\n  - h: expected format hostname"},
		{`{"type":"object","properties":{"r":{"type":"string","format":"regex"}}}`, `{"r":"["}`, "REJECT\n  - r: expected format regex"},
		{`{"type":"object","properties":{"i":{"type":"string","format":"uuid"}}}`, `{"i":"not"}`, "REJECT\n  - i: expected format uuid"},
		{`{"type":"object","properties":{"ip":{"type":"string","format":"ipv4"}}}`, `{"ip":"999.1.1.1"}`, "REJECT\n  - ip: expected format ipv4"},
	}
	for _, tc := range probes {
		got := validationOutcome(t, tc.schema, tc.args)
		if got != tc.want {
			t.Errorf("schema=%s args=%s\n got %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 packages/schema/src/shared.ts and compiler.ts. A
// pattern that is invalid in both ECMAScript modes fails schema compilation;
// boolean root schemas compile to true/false.
func TestValidateToolArgumentsCompilesPiSchema(t *testing.T) {
	invalid := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"s":{"type":"string","pattern":"["}}}`)}
	_, err := ValidateToolArguments(invalid, ToolCall{Name: "t", Arguments: json.RawMessage(`{"s":"y"}`)})
	if err == nil || !strings.Contains(err.Error(), "Invalid regular expression") {
		t.Fatalf("invalid pattern error = %v", err)
	}
	if _, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(`true`)}, ToolCall{Name: "t", Arguments: json.RawMessage(`1`)}); err != nil {
		t.Fatalf("true schema rejected: %v", err)
	}
	if _, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(`false`)}, ToolCall{Name: "t", Arguments: json.RawMessage(`1`)}); err == nil {
		t.Fatal("false schema accepted")
	}
	malformed := Tool{Name: "t", Parameters: json.RawMessage(`{"properties":{"a":1}}`)}
	if _, err := ValidateToolArguments(malformed, ToolCall{Name: "t", Arguments: json.RawMessage(`{"a":1}`)}); err == nil {
		t.Fatal("malformed subschema accepted")
	}
}

// Reference: pi v1.5.0 packages/ai/test/validation-probes.test.ts recursion
// cases. Local recursive references validate without expanding the schema.
func TestValidateToolArgumentsRecursiveRefs(t *testing.T) {
	tree := `{"type":"object","properties":{"t":{"$ref":"#/$defs/Tree"}},"$defs":{"Tree":{"anyOf":[{"type":"string"},{"type":"array","items":{"$ref":"#/$defs/Tree"}}]}}}`
	jsonSchema := `{"type":"object","properties":{"data":{"$ref":"#/$defs/Json"}},"required":["data"],"$defs":{"Json":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"array","items":{"$ref":"#/$defs/Json"}},{"type":"object","additionalProperties":{"$ref":"#/$defs/Json"}}]}}}`
	filter := `{"type":"object","properties":{"where":{"$ref":"#/$defs/F"}},"$defs":{"F":{"oneOf":[{"type":"object","properties":{"field":{"type":"string"},"eq":{"type":"string"}},"required":["field","eq"],"additionalProperties":false},{"type":"object","properties":{"and":{"type":"array","items":{"$ref":"#/$defs/F"}}},"required":["and"],"additionalProperties":false}]}}}`
	for _, tc := range []struct{ schema, args, want string }{
		{tree, `{"t":["a",["b"]]}`, `ok {"t":["a",["b"]]}`},
		{tree, `{"t":5}`, `ok {"t":"5"}`},
		{tree, `{"t":[5]}`, `ok {"t":["5"]}`},
		{jsonSchema, `{"data":{"a":[1,"x",{"b":true}]}}`, `ok {"data":{"a":[1,"x",{"b":true}]}}`},
		{jsonSchema, `{"data":null}`, `ok {"data":"null"}`},
		{jsonSchema, `{"data":{"a":[1,null]}}`, `ok {"data":{"a":[1,"null"]}}`},
		{filter, `{"where":{"and":[{"field":"a","eq":"b"}]}}`, `ok {"where":{"and":[{"field":"a","eq":"b"}]}}`},
		{filter, `{"where":{"and":[{"field":"a","eq":1,"x":2}]}}`, "REJECT\n  - where.and.0.eq: expected string, got integer"},
		{filter, `{"where":{"and":[{"and":[{"and":[{"and":[{"and":[{"field":1}]}]}]}]}]}}`, "REJECT\n  - where.and.0.and.0.and.0.and.0.and.0.eq: missing"},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("args=%s\n got %s\nwant %s", tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 packages/schema/src/formats. Tool schemas get every
// standard format check, not only email/uri/date-time.
func TestValidateToolArgumentsChecksStandardFormats(t *testing.T) {
	for _, tc := range []struct {
		format, value string
		valid         bool
	}{
		{"duration", "P3Y6M4DT12H30M5S", true},
		{"duration", "P", false},
		{"idn-email", "café@example.com", true},
		{"idn-email", "not-an-email", false},
		{"idn-hostname", "münich.example", true},
		{"idn-hostname", "bad_host", false},
		{"iri", "https://例子.测试/路径", true},
		{"iri", "not a url", false},
		{"iri-reference", "/路径", true},
		{"iri-reference", "not a url", false},
		{"relative-json-pointer", "1/foo", true},
		{"relative-json-pointer", "01/foo", false},
		{"uri-template", "http://example.com/{id}", true},
		{"uri-template", "http://example.com/{id", false},
		{"uri-reference", "a/b", true},
		{"uri-reference", "/a b", false},
		{"time", "23:59:59Z", true},
		{"time", "23:59:59", false},
		{"time", "24:00:00Z", false},
		{"date", "2024-02-29", true},
		{"date", "2023-02-29", false},
	} {
		schema := `{"type":"object","properties":{"v":{"type":"string","format":"` + tc.format + `"}}}`
		args := `{"v":` + string(mustMarshalJSONString(tc.value)) + `}`
		outcome := validationOutcome(t, schema, args)
		if tc.valid && !strings.HasPrefix(outcome, "ok ") {
			t.Errorf("%s %q: %s", tc.format, tc.value, outcome)
		}
		if !tc.valid && !strings.Contains(outcome, "expected format "+tc.format) {
			t.Errorf("%s %q: %s", tc.format, tc.value, outcome)
		}
	}
}

func mustMarshalJSONString(value string) []byte {
	encoded, _ := MarshalJSON(value)
	return encoded
}

// Reference: pi v1.5.0 packages/schema/src/annotations.ts. Prefix, items,
// contains and composed-schema evaluations mark array items and object
// properties as evaluated for unevaluatedItems/unevaluatedProperties.
func TestValidateToolArgumentsUnevaluatedAnnotations(t *testing.T) {
	for _, tc := range []struct{ schema, args, want string }{
		{`{"type":"object","properties":{"l":{"type":"array","prefixItems":[{"type":"number"}],"unevaluatedItems":false}}}`, `{"l":[1,2]}`, "REJECT\n  - l.1: unexpected item"},
		{`{"type":"object","properties":{"l":{"type":"array","prefixItems":[{"type":"number"}],"contains":{"type":"string"},"unevaluatedItems":false}}}`, `{"l":[1,"x"]}`, `ok {"l":[1,"x"]}`},
		{`{"type":"object","properties":{"l":{"type":"array","prefixItems":[{"type":"number"}],"contains":{"type":"string"},"unevaluatedItems":false}}}`, `{"l":[1,"x",2]}`, "REJECT\n  - l.2: unexpected item"},
		{`{"type":"object","properties":{"l":{"type":"array","allOf":[{"prefixItems":[{"type":"number"}]},{"prefixItems":[true,{"type":"string"}]}],"unevaluatedItems":false}}}`, `{"l":[1,"x"]}`, `ok {"l":[1,"x"]}`},
		{`{"type":"object","properties":{"l":{"type":"array","allOf":[{"prefixItems":[{"type":"number"}]},{"prefixItems":[true,{"type":"string"}]}],"unevaluatedItems":false}}}`, `{"l":[1,"x",3]}`, "REJECT\n  - l.2: unexpected item"},
		{`{"allOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"string"}}}],"unevaluatedProperties":false}`, `{"a":"x","b":"y"}`, `ok {"a":"x","b":"y"}`},
		{`{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"string"}}}],"unevaluatedProperties":false}`, `{"a":"x","b":"y"}`, `ok {"a":"x","b":"y"}`},
		{`{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"string"}}}],"unevaluatedProperties":false}`, `{"a":"x","b":"y","c":1}`, "REJECT\n  - c: unknown field"},
		{`{"type":"object","properties":{"l":{"type":"array","contains":{"type":"number"},"unevaluatedItems":{"type":"string"}}}}`, `{"l":[1,"x"]}`, `ok {"l":[1,"x"]}`},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s\n got %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 packages/schema/src/compiler.ts dynamicRef. A
// same-resource $dynamicAnchor or pointer resolves like $ref; a dangling
// dynamic reference rejects only values at its place.
func TestValidateToolArgumentsDynamicRef(t *testing.T) {
	anchored := `{"type":"object","properties":{"x":{"$dynamicRef":"#item"}},"$defs":{"item":{"$dynamicAnchor":"item","type":"string"}}}`
	if got := validationOutcome(t, anchored, `{"x":"x"}`); got != `ok {"x":"x"}` {
		t.Errorf("dynamic anchor: %s", got)
	}
	pointer := `{"type":"object","properties":{"x":{"$dynamicRef":"#/$defs/item"}},"$defs":{"item":{"type":"string"}}}`
	if got := validationOutcome(t, pointer, `{"x":"x"}`); got != `ok {"x":"x"}` {
		t.Errorf("dynamic pointer: %s", got)
	}
	missing := `{"type":"object","properties":{"x":{"$dynamicRef":"#missing"}}}`
	if got := validationOutcome(t, missing, `{"x":1}`); !strings.Contains(got, "does not match") {
		t.Errorf("missing dynamic anchor: %s", got)
	}
}
