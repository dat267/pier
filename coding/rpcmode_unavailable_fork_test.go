package coding

import "testing"

// D235 exposes a Go host with no replacement factory, unlike pi v1.4.0's
// modes/rpc/rpc-mode.ts runtime. Its explicit unavailable error must not mutate
// the current manager or create a branch before discovering the missing host.
func TestRPCUnavailableForkPreservesInMemorySession(t *testing.T) {
	for _, kind := range []string{"fork", "clone"} {
		t.Run(kind, func(t *testing.T) {
			session := rpcTestSession(t)
			firstID := session.Sessions.AppendMessage(createUserMessage("original"))
			session.Agent.SetMessages(session.Sessions.Projection().Messages)
			originalID := session.SessionID()
			command := `{"id":"change","type":"` + kind + `","entryId":"` + firstID + `"}`
			responses := rpcResponses(runRPCScript(t, session, command+"\n"+
				`{"id":"state","type":"get_state"}`+"\n"+
				`{"id":"entries","type":"get_entries"}`+"\n"))
			if len(responses) != 3 || responses[0]["success"] != false || responses[0]["error"] != "Session replacement is not available in this build" {
				t.Fatalf("replacement responses = %+v", responses)
			}
			state := responses[1]["data"].(map[string]any)
			if state["sessionId"] != originalID {
				t.Errorf("unavailable replacement changed session id: %v, want %s", state["sessionId"], originalID)
			}
			entries := responses[2]["data"].(map[string]any)["entries"].([]any)
			if len(entries) != 1 || entries[0].(map[string]any)["id"] != firstID {
				t.Errorf("unavailable replacement changed entries: %+v", entries)
			}
		})
	}
}
