package ai

import "testing"

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
