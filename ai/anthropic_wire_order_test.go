package ai

import (
	"encoding/json"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/anthropic-messages.ts
// convertTools spreads strict parameters before legacyInputSchema. Overwritten
// fields keep their original position; new legacy fields append.
func TestAnthropicStrictToolSchemaKeepsSpreadOrder(t *testing.T) {
	tools, err := ConvertAnthropicTools([]Tool{{
		Name: "lookup", Description: "Look up a value",
		Parameters: json.RawMessage(`{"title":"Lookup","type":"object","properties":{"z":{"type":"string"},"a":{"type":"number"}},"required":["z","a"]}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true,
			Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}},
	}}, false, true, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(tools)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"lookup","description":"Look up a value","eager_input_streaming":true,"strict":true,"input_schema":{"title":"Lookup","type":"object","properties":{"z":{"type":"string"},"a":{"type":"number"}},"required":["z","a"],"additionalProperties":false}}]`
	if string(encoded) != want {
		t.Fatalf("tools = %s\nwant    %s", encoded, want)
	}
}

// Reference: anthropic-messages.ts convertTools uses schema.properties ?? {},
// so explicit null and an absent properties member both become an empty map.
func TestAnthropicLegacyToolSchemaDefaultsNullProperties(t *testing.T) {
	tools, err := ConvertAnthropicTools([]Tool{{
		Name: "empty", Parameters: json.RawMessage(`{"type":"object","properties":null,"required":null}`),
	}}, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(tools)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"empty","description":"","input_schema":{"type":"object","properties":{},"required":[]}}]`
	if string(encoded) != want {
		t.Fatalf("tools = %s\nwant    %s", encoded, want)
	}
}
