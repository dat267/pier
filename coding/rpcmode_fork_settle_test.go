package coding

import (
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 core/agent-session-runtime.ts fork settles the old turn before
// mutating an in-memory manager in place. Its aborted tail must not be written
// into the newly selected branch. This exercises the JSONL RPC boundary.
func TestRPCInMemoryForkSettlesBeforeChangingBranch(t *testing.T) {
	session := blockingRPCSession(t)
	firstID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)
	entered := make(chan struct{})
	stream := session.Agent.StreamFunction
	session.Agent.StreamFunction = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		close(entered)
		return stream(model, transcript, options)
	}
	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"prompt","type":"prompt","message":"still running"}`)
	if response := client.until(t, byResponseID("prompt")); response["success"] != true {
		t.Fatalf("prompt response = %+v", response)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not enter provider stream")
	}
	client.send(t, `{"id":"fork","type":"fork","entryId":"`+firstID+`"}`)
	if response := client.until(t, byResponseID("fork")); response["success"] != true {
		t.Fatalf("fork response = %+v", response)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("fork before the first message retained the outgoing turn: %+v", messages)
	}
}
