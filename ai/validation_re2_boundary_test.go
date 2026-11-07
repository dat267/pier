package ai

import (
	"encoding/json"
	"testing"
)

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
