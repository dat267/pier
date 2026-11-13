package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) commit af79b0c3d added TypeBox's tryBigInt and
// tryUndefined to packages/schema/src/coerce.ts, and changed matchesType to
// Number.isFinite. The corpus in testdata/validation-pi covers the bigint
// probes; these tests pin the rules it cannot express, and the writer's JSON
// shape for the values JSON has no form for.

func validateProbe(t *testing.T, schema, arguments string) (string, error) {
	t.Helper()
	got, err := ValidateToolArguments(
		Tool{Name: "probe", Parameters: json.RawMessage(schema)},
		ToolCall{Name: "probe", Arguments: json.RawMessage(arguments)},
	)
	return string(got), err
}

func TestPiSchemaBigIntCoercion(t *testing.T) {
	const schema = `{"type":"object","properties":{"n":{"type":"bigint"}},"required":["n"]}`
	for _, test := range []struct{ arg, want string }{
		{`{"n":1}`, `{"n":"1n"}`},
		{`{"n":"12"}`, `{"n":"12n"}`},
		{`{"n":"-7n"}`, `{"n":"-7n"}`},
		{`{"n":"12.9"}`, `{"n":"12n"}`},
		{`{"n":true}`, `{"n":"1n"}`},
		{`{"n":null}`, `{"n":"0n"}`},
	} {
		got, err := validateProbe(t, schema, test.arg)
		if err != nil {
			t.Fatalf("%s: err = %v", test.arg, err)
		}
		if got != test.want {
			t.Fatalf("%s = %s, want %s", test.arg, got, test.want)
		}
	}
	// A string TypeBox's TryBigInt rejects stays an argument and fails the type
	// check, named the way explain.ts names it.
	if _, err := validateProbe(t, schema, `{"n":"1.2.3"}`); err == nil ||
		!strings.Contains(err.Error(), "expected bigint, got string") {
		t.Fatalf("err = %v, want a bigint rejection", err)
	}
}

func TestPiSchemaUndefinedCoercion(t *testing.T) {
	const schema = `{"type":"object","properties":{"n":{"type":"undefined"},"keep":{"type":"string"}}}`
	// JSON has no undefined: a converted property is dropped, and an array slot
	// becomes null, as JSON.stringify renders it.
	got, err := validateProbe(t, schema, `{"n":null,"keep":"x"}`)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != `{"keep":"x"}` {
		t.Fatalf("arguments = %s, want {\"keep\":\"x\"}", got)
	}
	got, err = validateProbe(t, `{"type":"array","items":{"type":"undefined"}}`, `[null,0]`)
	if err != nil {
		t.Fatalf("array err = %v", err)
	}
	if got != `[null,null]` {
		t.Fatalf("array = %s, want [null,null]", got)
	}
}

func TestPiSchemaRejectsNonFiniteNumbers(t *testing.T) {
	const schema = `{"type":"object","properties":{"n":{"type":"number"}},"required":["n"]}`
	_, err := validateProbe(t, schema, `{"n":1e999}`)
	if err == nil || !strings.Contains(err.Error(), "expected number, got Infinity") {
		t.Fatalf("err = %v, want a number rejection naming Infinity", err)
	}
}
