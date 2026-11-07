package coding

import (
	"strings"
	"testing"
)

// D382: pi v1.5.0 (f9a26365) core/sdk.ts builds the agent with `tools: []` and
// core/agent-session.ts installs the loadout per prompt, so an untouched RPC
// session reports `messageCount: 0`, `get_messages` returns `[]`, and `compact`
// reaches "Nothing to compact (session too small)". Pier installs the tools at
// construction, which places the leading system declaration message, and treats
// the placeholder model as absent, so `compact` fails with "No model selected".
func TestRPCFreshSessionHoldsTheToolDeclaration(t *testing.T) {
	persist := false
	cwd := t.TempDir()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: cwd, StreamFn: stubStreamFn, Tools: CreateCodingTools(cwd, nil),
		Sessions: NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist}),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := startRPCTest(t, session)
	client.send(t, `{"id":"state","type":"get_state"}`)
	state := client.until(t, func(record map[string]any) bool { return record["id"] == "state" })
	data := state["data"].(map[string]any)
	if data["messageCount"] != float64(1) {
		t.Errorf("messageCount = %v, want 1 (upstream 0)", data["messageCount"])
	}
	if _, ok := data["model"]; ok {
		t.Errorf("get_state sent the placeholder model: %v", data["model"])
	}

	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, func(record map[string]any) bool { return record["id"] == "messages" })
	list := messages["data"].(map[string]any)["messages"].([]any)
	if len(list) != 1 {
		t.Fatalf("get_messages = %d messages, want the declaration only (upstream [])", len(list))
	}
	declaration := list[0].(map[string]any)
	if declaration["role"] != "system" || declaration["content"] != "" || len(declaration["toolsAdded"].([]any)) == 0 {
		t.Errorf("first message = %v, want the empty system tool declaration", declaration)
	}

	client.send(t, `{"id":"compact","type":"compact"}`)
	compact := client.until(t, func(record map[string]any) bool { return record["id"] == "compact" })
	if compact["success"] != false || !strings.Contains(compact["error"].(string), "No model selected") {
		t.Errorf("compact = %v, want No model selected (upstream: Nothing to compact)", compact)
	}
}
