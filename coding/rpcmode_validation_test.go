package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D404: pi v1.5.0 (f9a26365) modes/rpc/rpc-mode.ts hands set_steering_mode and
// set_follow_up_mode `command.mode` and set_thinking_level `command.level` to
// the session without validating either, so "bogus" is stored and reported by
// get_state. Pier rejects a value it cannot honor and leaves the current mode
// and level unchanged.
func TestRPCRejectsModesAndThinkingLevelsItCannotHonor(t *testing.T) {
	session := rpcTestSession(t)
	session.Agent.SetModel(&ai.Model{ID: "m", Provider: "test", Reasoning: true})
	records := rpcResponses(runRPCScript(t, session, `{"id":"steer","type":"set_steering_mode","mode":"bogus"}
{"id":"follow","type":"set_follow_up_mode","mode":"bogus"}
{"id":"level","type":"set_thinking_level","level":"bogus"}
{"id":"state","type":"get_state"}
`))
	if len(records) != 4 {
		t.Fatalf("responses = %d, want 4", len(records))
	}
	for index, want := range []string{
		"mode must be all or one-at-a-time",
		"mode must be all or one-at-a-time",
		"level must be a valid thinking level",
	} {
		if records[index]["success"] != false || records[index]["error"] != want {
			t.Fatalf("response %d = %+v; want success false and %q", index, records[index], want)
		}
	}
	// The rejected values were not adopted: the session keeps what it had.
	state := records[3]["data"].(map[string]any)
	for _, key := range []string{"steeringMode", "followUpMode"} {
		if state[key] != "one-at-a-time" {
			t.Fatalf("%s = %v; want the unchanged default", key, state[key])
		}
	}
	if level, _ := state["thinkingLevel"].(string); level == "bogus" {
		t.Fatal("the session adopted an invalid thinking level")
	}
}

// D404: the reference reports success for a thinking-level command that omits
// the field (pi v1.5.2 modes/rpc/rpc-mode.ts passes the undefined value on and
// the level stays as it was). Pier treats a missing value like an invalid one:
// the command fails and the level is untouched.
func TestRPCRejectsAnOmittedThinkingLevel(t *testing.T) {
	session := rpcTestSession(t)
	session.Agent.SetModel(&ai.Model{ID: "m", Provider: "test", Reasoning: true})
	records := rpcResponses(runRPCScript(t, session, `{"id":"none","type":"set_thinking_level"}
{"id":"state","type":"get_state"}
`))
	if len(records) != 2 {
		t.Fatalf("responses = %d, want 2", len(records))
	}
	if records[0]["success"] != false || records[0]["error"] != "level must be a valid thinking level" {
		t.Fatalf("response 0 = %+v; want the invalid-level error", records[0])
	}
	state := records[1]["data"].(map[string]any)
	if level, _ := state["thinkingLevel"].(string); level == "" {
		t.Fatalf("thinkingLevel = %v; want the unchanged level", state["thinkingLevel"])
	}
}

// rpc-mode.ts:407 dispatches on command.type with no shape check beyond
// `typeof parsed === "object"`, which a JSON ARRAY satisfies. An array therefore
// reaches handleCommand, falls to the default branch, and answers
// `Unknown command: undefined` with no id. The port decoded the line into a
// map[string]json.RawMessage, which ERRORS for an array, so it answered a parse
// error instead: a different response for the same input.
func TestRPCArrayLineAnswersUnknownCommand(t *testing.T) {
	session := rpcTestSession(t)
	records := rpcResponses(runRPCScript(t, session, "[1,2]\n"))
	if len(records) != 1 {
		t.Fatalf("responses = %d, want 1: %+v", len(records), records)
	}
	response := records[0]
	if _, hasID := response["id"]; hasID {
		t.Errorf("response carries id %v; an array has no id member", response["id"])
	}
	if response["command"] != "" {
		t.Errorf("command = %v, want empty: an array has no type", response["command"])
	}
	if response["success"] != false {
		t.Errorf("success = %v, want false", response["success"])
	}
	if got, _ := response["error"].(string); !strings.Contains(got, "Unknown command") {
		t.Errorf("error = %q, want an unknown-command error", got)
	}

	// The same holds for every other JSON value: JSON.parse succeeds, `type` is
	// undefined, and the default branch answers. An object with an EMPTY type has
	// a defined type, so its message ends after the colon instead.
	for _, c := range []struct{ line, want string }{
		{`[]`, "Unknown command: undefined"},
		{`"str"`, "Unknown command: undefined"},
		{`42`, "Unknown command: undefined"},
		{`null`, "Unknown command: undefined"},
		{`{"type":""}`, "Unknown command: "},
	} {
		records := rpcResponses(runRPCScript(t, rpcTestSession(t), c.line+"\n"))
		if len(records) != 1 {
			t.Fatalf("%s: responses = %d, want 1", c.line, len(records))
		}
		if got, _ := records[0]["error"].(string); got != c.want {
			t.Errorf("%s: error = %q, want %q", c.line, got, c.want)
		}
	}
}
