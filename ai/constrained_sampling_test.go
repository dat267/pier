package ai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// pi v1.4.0 api/constrained-sampling.ts makeJsonSchemaNodeStrict recurses
// into properties before wrapping optional values in an anyOf object.
func TestStrictSchemaTransformsPropertySchemas(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{"optional", `{"type":"object","properties":{"value":{"type":"string"}}}`, `{"type":"object","properties":{"value":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["value"],"additionalProperties":false}`},
		{"nested", `{"type":"object","properties":{"nested":{"type":"object","properties":{"value":{"type":"number"}}}},"required":["nested"]}`, `{"type":"object","properties":{"nested":{"type":"object","properties":{"value":{"anyOf":[{"type":"number"},{"type":"null"}]}},"required":["value"],"additionalProperties":false}},"required":["nested"],"additionalProperties":false}`},
		{"numeric names", `{"type":"object","properties":{"10":{"type":"string"},"2":{"type":"number"}},"required":["10","2"]}`, `{"type":"object","properties":{"2":{"type":"number"},"10":{"type":"string"}},"required":["2","10"],"additionalProperties":false}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MakeStrictJSONSchema(json.RawMessage(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected any
			if err := json.Unmarshal(got, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.want), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("strict schema = %s; want %s", got, tt.want)
			}
		})
	}
}

// pi v1.4.0 api/constrained-sampling.ts checks schema.type === "object"
// and rejects null properties/required rather than treating them as missing.
func TestStrictSchemaRecognizesObjectAndArrayShapes(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		invalid     bool
	}{
		{"escaped type", `{"type":"ob\u006aect","properties":{}}`, false},
		{"nested union type", `{"type":"object","properties":{"x":{"type":["object"],"properties":{}}}}`, true},
		{"null properties", `{"type":"object","properties":null}`, true},
		{"null required", `{"type":"object","required":null}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MakeStrictJSONSchema(json.RawMessage(tt.input))
			if (err != nil) != tt.invalid {
				t.Fatalf("schema=%s error=%v; invalid=%v", out, err, tt.invalid)
			}
		})
	}
}

// pi v1.4.0 api/anthropic-messages.ts compares minItems to numeric 0/1.
func TestAnthropicStrictMinimumItemsUsesNumericValue(t *testing.T) {
	for _, raw := range []string{"0", "-0", "0.0", "1", "1.0", "1e0"} {
		if AnthropicStrictUnsupportedKeyword("minItems", json.RawMessage(raw)) {
			t.Errorf("minItems %s rejected", raw)
		}
	}
	for _, raw := range []string{"null", `"1"`, "true", "2"} {
		if !AnthropicStrictUnsupportedKeyword("minItems", json.RawMessage(raw)) {
			t.Errorf("minItems %s accepted", raw)
		}
	}
}

// buffer.Input is a byte prefix of nextInput, so the delta starts at its
// length. Upstream slices by buffer.input.length (UTF-16), and a Go string
// sliced at a UTF-16 offset would cut multi-byte text; the byte prefix makes
// len(buffer.Input) the exact offset.
func TestAppendGrammarToolInputJSONDeltaSlicesBytes(t *testing.T) {
	buffer := &GrammarToolInputJSONBuffer{}
	delta, ok, err := AppendGrammarToolInputJSONDelta(buffer, "payload", "日本", false)
	if !ok || err != nil {
		t.Fatalf("first delta: ok=%v err=%v", ok, err)
	}
	if delta != `{"payload":"日本` {
		t.Fatalf("first delta = %q", delta)
	}
	delta, ok, err = AppendGrammarToolInputJSONDelta(buffer, "payload", "日本語", true)
	if !ok || err != nil {
		t.Fatalf("second delta: ok=%v err=%v", ok, err)
	}
	if delta != `語"}` {
		t.Fatalf("second delta = %q", delta)
	}
}

// pi v1.4.0 api/constrained-sampling.ts includes the offending keyword value
// in the provider rejection: `${key}: ${JSON.stringify(value)} is unsupported`.
func TestStrictSamplingErrorIncludesTheRejectedValue(t *testing.T) {
	for _, testCase := range []struct{ schema, want string }{
		{
			`{"type":"object","properties":{"n":{"type":"number","maximum":10}},"required":["n"]}`,
			`Tool "t" requires JSON-schema constrained sampling, but maximum: 10 is unsupported.`,
		},
		{
			`{"type":"object","properties":{"s":{"type":"string","format":"weird"}},"required":["s"]}`,
			`Tool "t" requires JSON-schema constrained sampling, but format: "weird" is unsupported.`,
		},
		{
			`{"type":"object","properties":{"s":{"type":"string","format":5}},"required":["s"]}`,
			`Tool "t" requires JSON-schema constrained sampling, but format: 5 is unsupported.`,
		},
	} {
		tool := Tool{
			Name: "t", Parameters: json.RawMessage(testCase.schema),
			ConstrainedSampling: ConstrainedSamplingValue{
				Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "require"},
			},
		}
		_, _, err := ResolveJSONSchemaStrictSampling(tool, true, AnthropicStrictUnsupportedKeyword)
		if err == nil || err.Error() != testCase.want {
			t.Fatalf("schema %s err = %v; want %q", testCase.schema, err, testCase.want)
		}
	}
}

// A node with no type, no anyOf and no enum is rejected in strict mode: OpenAI
// rejects it, and a bare enum counts as typed while a bare const does not
// (packages/ai/src/api/constrained-sampling.ts, v1.6.0).
func TestStrictSchemaRejectsAnUntypedNode(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{"bare const", `{"type":"object","properties":{"a":{"const":null}},"required":["a"],"additionalProperties":false}`, true},
		{"bare enum is typed", `{"type":"object","properties":{"a":{"enum":["x","y"]}},"required":["a"],"additionalProperties":false}`, false},
		{"typed property", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":false}`, false},
		{"untyped root", `{"properties":{"a":{"type":"string"}}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MakeStrictJSONSchema(json.RawMessage(tc.schema))
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "schemas without a type are unsupported") {
					t.Fatalf("err = %v, want the untyped-node rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want a converted schema", err)
			}
		})
	}
}
