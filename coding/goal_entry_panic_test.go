package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/agent"
)

// panicOnceGoalSink models a session listener that throws on the first published
// goal entry. Agent loop tool execution recovers a thrown/panicking tool call
// into an error result (agent/loop.go), and upstream's synchronous dispatcher
// remains usable after a listener throws.
type panicOnceGoalSink struct {
	*fakeGoalSink
	panicNext bool
}

func (s *panicOnceGoalSink) EmitGoalEntryAppended(entry *SessionEntry) {
	if s.panicNext {
		s.panicNext = false
		panic("goal listener exploded")
	}
	s.fakeGoalSink.EmitGoalEntryAppended(entry)
}

func TestGoalPublisherResumesAfterRecoveredListenerPanic(t *testing.T) {
	sink := &panicOnceGoalSink{fakeGoalSink: &fakeGoalSink{}, panicNext: true}
	controller := newGoalController(nil, sink)
	tool := findGoalTool(t, controller, "set_goal")
	call := func(params string) (recovered any) {
		defer func() { recovered = recover() }()
		_, _ = tool.Execute("id", json.RawMessage(params), context.Background(), func(agent.AgentToolResult) {})
		return nil
	}

	if recovered := call(`{"objective":"first"}`); recovered == nil {
		t.Fatal("the listener panic did not reach the tool dispatcher")
	}
	if len(sink.entries) != 1 {
		t.Fatalf("durable entries = %d, want the first create", len(sink.entries))
	}
	if recovered := call(`{"objective":"second"}`); recovered != nil {
		t.Fatalf("second goal mutation panicked: %v", recovered)
	}
	if len(sink.emitted) != 2 {
		t.Fatalf("published entries after recovery = %d, want the replacement clear/create pair", len(sink.emitted))
	}
}
