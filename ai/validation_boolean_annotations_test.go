package ai

import "testing"

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
