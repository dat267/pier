package coding

import (
	"encoding/json"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), core/tools/edit.ts
// prepareEditArguments replaces a JSON-string edits field only when the
// parsed value is an array or a single-edit object, never for JSON null.
func TestPrepareEditArgumentsLeavesJSONStringNullForValidation(t *testing.T) {
	input := json.RawMessage(`{"path":"p","edits":"null"}`)
	if got := string(PrepareEditArguments(input)); got != string(input) {
		t.Fatalf("prepared arguments = %s, want unchanged %s", got, input)
	}
	control := json.RawMessage(`{"path":"p","edits":"[]"}`)
	if got := string(PrepareEditArguments(control)); got != `{"path":"p","edits":[]}` {
		t.Fatalf("array-string control = %s, want parsed edits array", got)
	}
}
