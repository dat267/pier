package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

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
