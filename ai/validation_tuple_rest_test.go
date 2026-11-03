package ai

import "testing"

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
