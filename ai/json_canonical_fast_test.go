package ai

import "testing"

func TestCanonicalJSONIsNormalized(t *testing.T) {
	normalized := []string{
		`{}`, `[]`, `{"a":1,"b":[true,null,"x"]}`,
		`{"1":1,"2":2,"a":3}`,
		`"\u0001"`, `"\b\f\n\r\t"`, `1.5`, `1e-7`, "\"\u2028\"", "\"\x7f\"",
	}
	for _, input := range normalized {
		if !canonicalJSONIsNormalized([]byte(input)) {
			t.Errorf("canonicalJSONIsNormalized(%q) = false", input)
		}
	}
	noncanonical := []string{
		` null`, `{"a": 1}`, `{"a":1,"a":2}`,
		`{"a":1,"1":2}`, `{"2":1,"1":2}`, `{"1":"\u0031"}`,
		`"\u000A"`, `"\u003c"`, `"\/"`, `"\u2028"`,
		`01`, `1.0`, `-0`, `1e2`, `1E2`, "\"\xff\"", ``,
	}
	for _, input := range noncanonical {
		if canonicalJSONIsNormalized([]byte(input)) {
			t.Errorf("canonicalJSONIsNormalized(%q) = true", input)
		}
	}
}

func TestCanonicalizeNormalizedFastPathOwnsItsBytes(t *testing.T) {
	input := []byte(`{"a":"<&>","b":[1,1.5]}`)
	got, err := canonicalJavaScriptJSON(input)
	if err != nil || string(got) != string(input) {
		t.Fatalf("canonical = %q/%v, want %q", got, err, input)
	}
	got[0] = '['
	if input[0] != '{' {
		t.Fatal("fast path returned the caller's buffer instead of an owned copy")
	}
}
