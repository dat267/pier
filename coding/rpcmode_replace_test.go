package coding

// Session-replacement RPC commands: new_session, switch_session, fork, clone
// (pi v1.3.0 modes/rpc/rpc-mode.ts + core/agent-session-runtime.ts).

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/dat267/pier/ai"
)

// rpcPersistentSession builds a session whose manager persists into its own
// directory, so it has a session file to open, branch and clone.
func rpcPersistentSession(t *testing.T) *AgentSession {
	t.Helper()
	cwd := t.TempDir()
	persist := true
	manager := NewSessionManager(cwd, &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
	session, err := NewAgentSession(&SessionConfig{Cwd: cwd, StreamFn: stubStreamFn, Sessions: manager})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// rpcReplaceFactory recreates a session for a replacement manager the way the
// CLI factory does, restoring the manager's messages.
func rpcReplaceFactory() func(context.Context, *SessionManager) (*AgentSession, error) {
	return func(_ context.Context, manager *SessionManager) (*AgentSession, error) {
		session, err := NewAgentSession(&SessionConfig{Cwd: manager.GetCwd(), StreamFn: stubStreamFn, Sessions: manager})
		if err != nil {
			return nil, err
		}
		session.Agent.SetMessages(manager.Projection().Messages)
		return session, nil
	}
}

func byResponseID(id string) func(map[string]any) bool {
	return func(record map[string]any) bool {
		return record["type"] == "response" && record["id"] == id
	}
}

// pi v1.3.0 new_session starts a fresh session in the current directory and
// rebinds the runtime to it.
func TestRPCNewSession(t *testing.T) {
	session := rpcPersistentSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "old"}})
	originalID := session.SessionID()

	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"n","type":"new_session"}`)
	response := client.until(t, byResponseID("n"))
	if response["success"] != true || response["data"].(map[string]any)["cancelled"] != false {
		t.Fatalf("new_session response = %+v", response)
	}
	client.send(t, `{"id":"state","type":"get_state"}`)
	state := client.until(t, byResponseID("state"))["data"].(map[string]any)
	if state["sessionId"] == originalID {
		t.Fatalf("session id unchanged: %v", state["sessionId"])
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("new session should be empty: %+v", messages)
	}
}

// pi v1.3.0 switch_session opens an existing session file and rebinds to it.
func TestRPCSwitchSession(t *testing.T) {
	cwd := t.TempDir()
	persist := true
	target := NewSessionManager(cwd, &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
	target.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "resumed"}})
	targetPath := target.GetSessionFile()
	if targetPath == "" {
		t.Fatal("target session has no file")
	}

	session := rpcPersistentSession(t)
	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"s","type":"switch_session","sessionPath":`+strconv.Quote(targetPath)+`}`)
	response := client.until(t, byResponseID("s"))
	if response["success"] != true || response["data"].(map[string]any)["cancelled"] != false {
		t.Fatalf("switch_session response = %+v", response)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("resumed messages = %+v", messages)
	}
}

// pi v1.3.0 fork with position "before" branches at the message's parent and
// returns its text for resending.
func TestRPCForkBefore(t *testing.T) {
	session := rpcPersistentSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop,
	})
	secondID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)

	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"f","type":"fork","entryId":"`+secondID+`"}`)
	response := client.until(t, byResponseID("f"))
	data := response["data"].(map[string]any)
	if data["text"] != "second" || data["cancelled"] != false {
		t.Fatalf("fork response = %+v", data)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("forked messages = %+v", messages)
	}
}

// pi v1.3.0 fork with position "at" keeps the named entry and omits selectedText.
func TestRPCForkAt(t *testing.T) {
	session := rpcPersistentSession(t)
	firstID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)

	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"f","type":"fork","entryId":"`+firstID+`","position":"at"}`)
	response := client.until(t, byResponseID("f"))
	data := response["data"].(map[string]any)
	if _, present := data["text"]; present {
		t.Fatalf("position at should omit text: %+v", data)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("forked messages = %+v", messages)
	}
}

// pi v1.3.0 clone forks at the current leaf.
func TestRPCClone(t *testing.T) {
	session := rpcPersistentSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)

	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"c","type":"clone"}`)
	response := client.until(t, byResponseID("c"))
	if response["success"] != true || response["data"].(map[string]any)["cancelled"] != false {
		t.Fatalf("clone response = %+v", response)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("cloned messages = %+v", messages)
	}
}

// pi v1.3.0 clone without a leaf fails explicitly.
func TestRPCCloneWithoutEntry(t *testing.T) {
	client := startRPCTestWith(t, rpcPersistentSession(t), RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"c","type":"clone"}`)
	response := client.until(t, byResponseID("c"))
	if response["success"] != false || response["error"] != "Cannot clone session: no current entry selected" {
		t.Fatalf("clone response = %+v", response)
	}
}

// pi v1.3.0 fork rejects a missing entry and a non-user "before" entry.
func TestRPCForkInvalidEntry(t *testing.T) {
	session := rpcPersistentSession(t)
	assistantID := session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop,
	})
	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"missing","type":"fork","entryId":"nope"}`)
	client.send(t, `{"id":"assistant","type":"fork","entryId":"`+assistantID+`"}`)
	first := client.until(t, byResponseID("missing"))
	second := client.until(t, byResponseID("assistant"))
	for _, response := range []map[string]any{first, second} {
		if response["success"] != false || response["error"] != "Invalid entry ID for forking" {
			t.Fatalf("fork response = %+v", response)
		}
	}
}

// pi v1.3.0 replacement commands fail explicitly when the host supplies no
// factory (the session cannot be rebuilt).
func TestRPCSessionReplacementUnavailable(t *testing.T) {
	session := rpcPersistentSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "entry"}})
	client := startRPCTest(t, session)
	client.send(t, `{"id":"n","type":"new_session"}`)
	client.send(t, `{"id":"c","type":"clone"}`)
	for _, id := range []string{"n", "c"} {
		response := client.until(t, byResponseID(id))
		if response["success"] != false || response["error"] != "Session replacement is not available in this build" {
			t.Fatalf("replacement response = %+v", response)
		}
	}
}

// pi v1.3.0 forks an in-memory session in place: the same manager is branched
// and there is still no session file afterwards.
func TestRPCForkInMemory(t *testing.T) {
	session := rpcTestSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	secondID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)

	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"f","type":"fork","entryId":"`+secondID+`"}`)
	if response := client.until(t, byResponseID("f")); response["success"] != true {
		t.Fatalf("fork response = %+v", response)
	}
	client.send(t, `{"id":"state","type":"get_state"}`)
	state := client.until(t, byResponseID("state"))["data"].(map[string]any)
	if _, present := state["sessionFile"]; present {
		t.Fatalf("in-memory fork should have no session file: %+v", state)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("forked messages = %+v", messages)
	}
}

// A failed replacement keeps the settled old session bound so the client can
// keep working.
func TestRPCReplacementFailureKeepsSession(t *testing.T) {
	session := rpcPersistentSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)
	originalID := session.SessionID()
	failing := func(context.Context, *SessionManager) (*AgentSession, error) {
		return nil, errors.New("boom")
	}
	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: failing})
	client.send(t, `{"id":"n","type":"new_session"}`)
	response := client.until(t, byResponseID("n"))
	if response["success"] != false || response["error"] != "boom" {
		t.Fatalf("new_session response = %+v", response)
	}
	client.send(t, `{"id":"state","type":"get_state"}`)
	state := client.until(t, byResponseID("state"))["data"].(map[string]any)
	if state["sessionId"] != originalID {
		t.Fatalf("session should be preserved: %+v", state)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("session messages = %+v", messages)
	}
}
