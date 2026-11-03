package ai

import "testing"

// Reference: pi v1.5.0 f9a26365 packages/schema/src/coerce.ts convertArray
// and reenter. A wrapped item is converted once, including const/enum rules.
// Repeated primitive values still cut off recursive conversions.
func TestValidateToolArgumentsConvertsWrappedItems(t *testing.T) {
	literal := `{"type":"object","properties":{"a":{"type":"array","items":{"const":1}}}}`
	recursive := `{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}},"$defs":{"A":{"anyOf":[{"type":"array","items":{"$ref":"#/$defs/A"}}]}}}`
	for _, tc := range []struct{ schema, args, want string }{
		{literal, `{"a":"1"}`, `ok {"a":[1]}`},
		{literal, `{"a":["1"]}`, `ok {"a":[1]}`},
		{recursive, `{"a":1}`, "REJECT\n  - a: expected array, got integer"},
	} {
		if got := validationOutcome(t, tc.schema, tc.args); got != tc.want {
			t.Errorf("schema=%s args=%s\ngot %s\nwant %s", tc.schema, tc.args, got, tc.want)
		}
	}
}
