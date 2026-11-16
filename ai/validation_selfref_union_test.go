package ai

import (
	"encoding/json"
	"testing"
)

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
