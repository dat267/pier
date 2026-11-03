package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 packages/schema/src/checks.ts isMultiple
// scales decimal coefficients with BigInt, so a tiny non-multiple is rejected
// without overflowing its divisor or panicking.
func TestValidateToolArgumentsRejectsTinyNonMultiple(t *testing.T) {
	tool := Tool{Name: "t", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"number","multipleOf":3}},"required":["n"]}`)}
	_, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"n":1e-100}`)})
	if err == nil || !strings.Contains(err.Error(), "n: expected a multiple of 3") {
		t.Fatalf("tiny non-multiple error = %v", err)
	}
	got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(`{"n":6}`)})
	if err != nil || string(got) != `{"n":6}` {
		t.Fatalf("valid multiple = %s, %v", got, err)
	}
}
