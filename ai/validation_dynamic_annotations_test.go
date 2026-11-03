package ai

import "testing"

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
