package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// A live transcript draws a goal card from the entry-appended event (upstream
// appends through the extension entry API, which emits entry_appended). The
// controller therefore publishes every durable goal entry after unlocking; the
// card cannot wait for a reload.
func TestGoalControllerEmitsEntryAppendedForDurableEntries(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"ship it"}`)

	if len(sink.emitted) != 1 {
		t.Fatalf("emitted entries = %d, want the create entry", len(sink.emitted))
	}
	entry := sink.emitted[0]
	if entry == nil || entry.Type != "custom" || entry.CustomType != goal.GoalCustomType {
		t.Fatalf("emitted entry = %+v", entry)
	}
	var change goal.GoalChangeEntry
	if err := json.Unmarshal(entry.Data, &change); err != nil {
		t.Fatalf("emitted entry payload: %v", err)
	}
	if change.Operation != goal.OpCreate || change.Goal == nil || change.Goal.Objective != "ship it" {
		t.Fatalf("emitted payload = %+v", change)
	}

	// A status change publishes its durable entry too.
	goalToolText(t, controller, "set_goal", `{"status":"complete"}`)
	if len(sink.emitted) != 2 || sink.emitted[1].CustomType != goal.GoalCustomType {
		t.Fatalf("emitted after complete = %d", len(sink.emitted))
	}
}

// The live session hands listeners the appended entry, so the interactive
// transcript renders the card without a reload.
func TestAgentSessionPublishesGoalEntries(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	streamFn := func(requestModel *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}
	var err error
	session, err = NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{testTool("read")}, ConvertToLlm: ConvertToLlm, Goal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var published []*SessionEntry
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionEntryAppended && event.Entry != nil && event.Entry.Type == "custom" {
			published = append(published, event.Entry)
		}
	})
	if _, err := session.control.Tools["set_goal"].Tool.Execute("create",
		json.RawMessage(`{"objective":"ship it"}`), context.Background(), func(agent.AgentToolResult) {}); err != nil {
		t.Fatal(err)
	}
	if len(published) != 1 || published[0].CustomType != goal.GoalCustomType {
		t.Fatalf("published entries = %+v", published)
	}
	if len(published[0].Data) == 0 {
		t.Fatal("published entry carries no payload for the renderer")
	}
}
