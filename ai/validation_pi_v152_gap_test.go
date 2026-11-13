package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// D471: the pi-schema port is pi v1.5.0 (f9a26365) and lags the v1.5.2 pin.
// These tests pin what the port does today so the divergence is recorded
// instead of silent: ai/testdata/validation-pi/probes.json was dumped from
// v1.5.0, so the corpus cannot see these.

// Upstream v1.5.2 (b4afdddf) commit af79b0c3d added TypeBox's TryBigInt and
// TryUndefined to packages/schema/src/coerce.ts, so a bigint schema converts
// 1 to "1n" and "12" to "12n" instead of rejecting. Pier has no bigint value in
// its schemaValue model and still rejects.
func TestPiSchemaPortKeepsV150BigIntRejection(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"bigint"}},"required":["n"]}`)
	for _, arg := range []string{`{"n":1}`, `{"n":"12"}`} {
		_, err := ValidateToolArguments(Tool{Name: "bigint", Parameters: schema}, ToolCall{Name: "bigint", Arguments: json.RawMessage(arg)})
		if err == nil || !strings.Contains(err.Error(), "expected bigint") {
			t.Fatalf("%s: err = %v, want a bigint rejection", arg, err)
		}
	}
}

// Upstream v1.5.2 changed matchesType in packages/schema/src/coerce.ts from
// `typeof value === "number"` to `Number.isFinite(value)`, so a non-finite
// argument no longer matches "number". Pier accepts it and the JSON writer
// renders the infinity as null, which is what v1.5.0 did.
func TestPiSchemaPortKeepsV150NonFiniteNumber(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"number"}},"required":["n"]}`)
	got, err := ValidateToolArguments(Tool{Name: "n", Parameters: schema}, ToolCall{Name: "n", Arguments: json.RawMessage(`{"n":1e999}`)})
	if err != nil {
		t.Fatalf("err = %v, want the v1.5.0 acceptance", err)
	}
	if string(got) != `{"n":null}` {
		t.Fatalf("arguments = %s, want {\"n\":null}", got)
	}
}
