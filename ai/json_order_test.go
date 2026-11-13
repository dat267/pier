package ai

import (
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252). Upstream builds request params objects and
// rewrites JSON schemas as plain JavaScript objects, so JSON.stringify orders
// array-index keys first (ascending), then the other members in first-assignment
// order. The shared ordered-object type serves both the wire builders and the
// schema rewriters.
func TestOrderedJSONFieldsMatchJavaScriptKeyOrder(t *testing.T) {
	fields, err := decodeOrderedJSONFields([]byte(`{"b":1,"2":2,"a":3,"1":4}`))
	if err != nil {
		t.Fatal(err)
	}
	// Re-assigning an existing member keeps its position; a new name appends.
	fields.set("b", []byte(`9`))
	fields.set("c", []byte(`5`))
	encoded, err := fields.marshal()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"1":4,"2":2,"b":9,"a":3,"c":5}`; got != want {
		t.Fatalf("marshal = %s, want %s", got, want)
	}

	// Removing a member keeps the remaining declaration order.
	fields.remove("a")
	encoded, err = fields.marshal()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"1":4,"2":2,"b":9,"c":5}`; got != want {
		t.Fatalf("marshal after remove = %s, want %s", got, want)
	}
}

// D468: the reference's AssistantMessage is built with a usage object, so a
// message it writes always carries one; pier models usage as a struct field, so
// a message read from a file without usage is re-serialized with the zero object
// (input/output/cacheRead/cacheWrite/totalTokens and a zero cost). Dropping it
// would need a custom marshaler over the field order that persisted sessions
// depend on, for a file upstream never writes.
func TestAssistantMessageAlwaysCarriesUsage(t *testing.T) {
	message := &AssistantMessage{
		Content:    ContentList{TextContent{Text: "hi"}},
		API:        "openai",
		Provider:   "openai",
		Model:      "gpt",
		StopReason: StopStop,
		Timestamp:  1700000000000,
	}
	encoded, err := MarshalMessage(message)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`) {
		t.Fatalf("message = %s, want the zero usage object", encoded)
	}
}
