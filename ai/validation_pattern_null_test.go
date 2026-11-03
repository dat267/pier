package ai

import "testing"

// Reference: pi v1.5.0 f9a26365 packages/schema/src/coerce.ts dropOptionalNulls.
// additionalProperties never applies to a key matched by patternProperties,
// even in the optional-null cleanup pass.
func TestValidateToolArgumentsPreservesPatternMatchedNulls(t *testing.T) {
	schema := `{"type":"object","patternProperties":{"^a$":{}},"additionalProperties":{"type":"object","properties":{"n":{"type":"integer"}}}}`
	args := `{"a":{"n":null},"b":{"n":null}}`
	want := `ok {"a":{"n":null},"b":{}}`
	if got := validationOutcome(t, schema, args); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}
