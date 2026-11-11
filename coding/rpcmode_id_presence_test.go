package coding

import "testing"

// TestRPCResponseEchoesTheIdOnlyWhenPresent pins the documented correlation
// contract in ARCHITECTURE.md's "Supported commands": "Every command accepts an
// optional string `id`. Responses repeat it when present, including an empty
// string. Omitted ids remain omitted." The distinction matters to a client that
// matches responses to requests: a response must never invent an id, and an
// empty one is still a value the client chose.
func TestRPCResponseEchoesTheIdOnlyWhenPresent(t *testing.T) {
	session := rpcTestSession(t)
	defer session.Dispose()
	records := runRPCScript(t, session, `{"id":"","type":"get_state"}
{"type":"get_state"}
{"id":"abc","type":"get_state"}
`)

	var responses []map[string]any
	for _, record := range records {
		if record["type"] == "response" && record["command"] == "get_state" {
			responses = append(responses, record)
		}
	}
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3: %v", len(responses), records)
	}

	id, present := responses[0]["id"]
	if !present || id != "" {
		t.Errorf(`the response to {"id":""} has id %v (present %v), want the empty string`, id, present)
	}
	if _, present := responses[1]["id"]; present {
		t.Errorf("the response to a command without an id carries id %v, want the property omitted", responses[1]["id"])
	}
	if id := responses[2]["id"]; id != "abc" {
		t.Errorf(`the response to {"id":"abc"} has id %v, want "abc"`, id)
	}
}
