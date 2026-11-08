package coding

// D235: the stdio RPC surface is a bounded subset. The reference only casts the
// parsed JSON to RpcCommand (pi v1.5.0 modes/rpc/rpc-mode.ts), so a wrong field
// type either coerces (`enabled: "yes"` turns compaction on) or fails later
// with a TypeError. Pier validates the fields the command cannot honor and
// rejects the command instead. These tests pin that contract, one row per
// documented rejection in ARCHITECTURE.md, so the messages cannot drift
// silently.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRPCRejectsFieldTypesItCannotHonor(t *testing.T) {
	cases := []struct {
		name        string
		line        string
		wantCommand string
		wantErr     string
	}{
		{"prompt without message", `{"type":"prompt"}`, "prompt", "message must be a string"},
		{"steer without message", `{"type":"steer"}`, "steer", "message must be a string"},
		{"follow-up with an unknown behavior", `{"type":"follow_up","message":"m","streamingBehavior":"nope"}`, "follow_up", "streamingBehavior must be steer or followUp"},
		{"bash without command", `{"type":"bash"}`, "bash", "command must be a string"},
		{"bash with a numeric command", `{"type":"bash","command":5}`, "bash", "Invalid command"},
		{"non-boolean enabled", `{"type":"set_auto_compaction","enabled":"yes"}`, "set_auto_compaction", "Invalid enabled"},
		{"missing enabled", `{"type":"set_auto_retry"}`, "set_auto_retry", "enabled must be a boolean"},
		{"switch without a path", `{"type":"switch_session"}`, "switch_session", "sessionPath must be a string"},
		{"fork without an entry", `{"type":"fork"}`, "fork", "entryId must be a string"},
		{"set_model without a model", `{"type":"set_model","provider":"p"}`, "set_model", "provider and modelId must be nonempty strings"},
		{"malformed images", `{"type":"prompt","message":"x","images":"nope"}`, "prompt", "Invalid images"},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			session := rpcTestSession(t)
			before := rpcResponseData(t, runRPCScript(t, session, `{"id":"before","type":"get_state"}`+"\n"))
			line := strings.Replace(testCase.line, `"type"`, `"id":"case","type"`, 1)
			responses := rpcResponses(runRPCScript(t, session, line+"\n"))
			if len(responses) != 1 {
				t.Fatalf("responses = %+v", responses)
			}
			response := responses[0]
			if response["success"] != false {
				t.Fatalf("malformed command succeeded: %+v", response)
			}
			message, _ := response["error"].(string)
			if !strings.Contains(message, testCase.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", message, testCase.wantErr)
			}
			// The response echoes the rejected command type.
			if response["command"] != testCase.wantCommand {
				t.Fatalf("command = %v, want %s", response["command"], testCase.wantCommand)
			}
			// A rejection must not change the session, and must not take the
			// command loop down with it.
			after := rpcResponseData(t, runRPCScript(t, session, `{"id":"after","type":"get_state"}`+"\n"))
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("case %d changed the session: before=%+v after=%+v", index, before, after)
			}
		})
	}
}

// The control for the table above: a well-formed toggle still succeeds and
// changes what get_state reports, so the unchanged-state assertion is meaningful.
func TestRPCValidToggleChangesTheReportedState(t *testing.T) {
	session := rpcTestSession(t)
	before := rpcResponseData(t, runRPCScript(t, session, `{"id":"before","type":"get_state"}`+"\n"))
	target, _ := before["autoCompactionEnabled"].(bool)
	records := runRPCScript(t, session, fmt.Sprintf(`{"id":"toggle","type":"set_auto_compaction","enabled":%v}`, !target)+"\n")
	response := rpcResponses(records)[0]
	if response["success"] != true {
		t.Fatalf("valid toggle failed: %+v", response)
	}
	after := rpcResponseData(t, runRPCScript(t, session, `{"id":"after","type":"get_state"}`+"\n"))
	if before["autoCompactionEnabled"] == after["autoCompactionEnabled"] {
		t.Fatalf("autoCompactionEnabled did not change: before=%v after=%v", before["autoCompactionEnabled"], after["autoCompactionEnabled"])
	}
}
