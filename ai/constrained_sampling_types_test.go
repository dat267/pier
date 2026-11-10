package ai

import (
	"encoding/json"
	"testing"
)

// The reference keeps a type array's members whatever the others are (pi
// v1.5.0 f9a26365 api/constrained-sampling.ts:40): the membership tests compare
// against strings, so a non-string member can never match. Decoding into
// []string dropped the whole array when one member was not a string, so
// isStructuredSchema answered false and the anyOf guard let an object union
// through. Keeping the string members answers identically.
func TestSchemaTypesKeepsStringsFromAMixedArray(t *testing.T) {
	for _, tc := range []struct {
		schema string
		want   []string
	}{
		{`"string"`, []string{"string"}},
		{`["string","null"]`, []string{"string", "null"}},
		{`["object",5]`, []string{"object"}},
		// JS keeps the null literal too; only string members can ever match a
		// type name, so keeping the strings is behaviorally the same.
		{`["string",null,"array"]`, []string{"string", "array"}},
		{`[]`, nil},
		{`[1,2]`, nil},
		{`123`, nil},
		{`null`, nil},
	} {
		got := schemaTypes(json.RawMessage(tc.schema))
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if len(got) != len(tc.want) {
			t.Fatalf("schemaTypes(%s) = %v, want %v", tc.schema, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("schemaTypes(%s) = %v, want %v", tc.schema, got, tc.want)
			}
		}
	}
}

func TestMixedTypeArrayIsAStructuredUnion(t *testing.T) {
	mixed := json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":["object",5]}]}}}`)
	if _, err := MakeStrictJSONSchema(mixed); err == nil {
		t.Fatal("a mixed type array containing object must be rejected as an object union")
	}
	// The control: a plain object variant is rejected by both implementations.
	plain := json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":"object"}]}}}`)
	if _, err := MakeStrictJSONSchema(plain); err == nil {
		t.Fatal("an object anyOf variant must be rejected")
	}
	// A mixed array with no object or array member stays allowed.
	ok := json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":["string",5]}]}}}`)
	if _, err := MakeStrictJSONSchema(ok); err != nil {
		t.Fatalf("a string union must stay allowed: %v", err)
	}
}
