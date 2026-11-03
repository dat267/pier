package ai

import "testing"

// Reference: pi v1.5.0 f9a26365 packages/schema/src/compiler.ts arrayGroup
// and coerce.ts convertArray. Empty tuple arrays are present tuples, not
// missing item schemas, and therefore never wrap a scalar into an array.
func TestValidateToolArgumentsHonorsEmptyTuples(t *testing.T) {
	closed := `{"type":"object","properties":{"a":{"type":"array","items":[],"additionalItems":false}}}`
	prefix := `{"type":"object","properties":{"a":{"type":"array","prefixItems":[]}}}`
	for _, tc := range []struct{ schema, args, want string }{
		{closed, `{"a":[1]}`, "REJECT\n  - a.0: no value is allowed here"},
		{closed, `{"a":"1"}`, "REJECT\n  - a: expected array, got string"},
		{closed, `{"a":[]}`, `ok {"a":[]}`},
		{prefix, `{"a":"1"}`, "REJECT\n  - a: expected array, got string"},
		{prefix, `{"a":[1]}`, `ok {"a":[1]}`},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s\ngot %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}
