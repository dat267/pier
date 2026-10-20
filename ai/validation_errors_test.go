package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.3.0 utils/validation.ts formatValidationPath and TypeBox
// 1.3.27 en_US diagnostics. These literals were checked against its source.
func TestValidationErrorPrintsParsedOriginalArguments(t *testing.T) {
	_, err := ValidateToolArguments(Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`)}, ToolCall{Name: "t", Arguments: json.RawMessage(`{"n":"\u0074wo"}`)})
	want := "Validation failed for tool \"t\":\n  - n: must be integer\n\nReceived arguments:\n{\n  \"n\": \"two\"\n}"
	if err == nil || err.Error() != want {
		t.Fatalf("error=%v; want %s", err, want)
	}
}

func TestValidationErrorsPointToFailingArgument(t *testing.T) {
	for _, tc := range []struct{ schema, args, lines string }{
		{`{"type":"object","properties":{"n":{"type":["integer"]}}}`, `{"n":"bad"}`, "  - n: must be either integer"},
		{`{"type":"object","properties":{"n":{"const":1}}}`, `{"n":2}`, "  - n: must be equal to constant"},
		{`{"type":"object","properties":{"n":{"enum":[1]}}}`, `{"n":2}`, "  - n: must be equal to one of the allowed values"},
		{`{"type":"object","properties":{"n":false}}`, `{"n":2}`, "  - n: schema is false"},
		{`{"type":"object","additionalProperties":false}`, `{"extra":2}`, "  - extra: schema is false\n  - root: must not have additional properties"},
		{`{"type":"object","properties":{"n":{"anyOf":[{"type":"integer"},{"type":"null"}]}}}`, `{"n":"bad"}`, "  - n: must be integer\n  - n: must be null\n  - n: must match a schema in anyOf"},
		{`{"type":"object","properties":{"n":{"oneOf":[{"type":"number"},{"type":"integer"}]}}}`, `{"n":1}`, "  - n: must match exactly one schema in oneOf"},
		{`{"type":"object","properties":{"n":{"allOf":[{"type":"integer"},{"const":1}]}}}`, `{"n":"bad"}`, "  - n: must be integer\n  - n: must be equal to constant"},
		{`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`, `{"count":"two"}`, "  - count: must be integer"},
		{`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`, `{}`, "  - count: must have required properties count"},
		{`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"count":{"type":"integer"}}}}}}`, `{"items":[{"count":"two"}]}`, "  - items.0.count: must be integer"},
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
