package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D223: a turn can run several objective set_goal calls in parallel. Each
// success must remove only the durable cards of the goal it reports, so a
// finish that arrives out of order cannot hide another call's change.
func TestGoalConcurrentObjectiveCallsRemoveOnlyTheirCards(t *testing.T) {
	dispatcher, _, renderer, _ := newEventTestDispatcher(t)
	renderer.EntryRenderer = GoalEntryRenderer
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
	session, err := coding.NewAgentSession(&coding.SessionConfig{
		Cwd: manager.GetCwd(), Sessions: manager, Goal: true,
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			t.Fatal("setting a goal must not start a model request")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Dispose)
	dispatcher.SessionInfo = manager
	defer session.Subscribe(dispatcher.HandleEvent)()

	objectives := []string{"first", "second"}
	calls := []string{"call-a", "call-b"}
	for index, id := range calls {
		arguments, err := json.Marshal(map[string]string{"objective": objectives[index]})
		if err != nil {
			t.Fatal(err)
		}
		dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionStart,
			Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: id, Args: arguments}})
	}
	tool := session.GetToolDefinition("set_goal")
	var results []agent.AgentToolResult
	for _, objective := range objectives {
		params, err := json.Marshal(map[string]string{"objective": objective})
		if err != nil {
			t.Fatal(err)
		}
		result, err := tool.Execute("call", params, context.Background(), nil)
		if err != nil || result.IsError {
			t.Fatalf("set_goal: result=%+v err=%v", result, err)
		}
		results = append(results, result)
	}
	// Finish the second call first: it must not consume the first call's cards.
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionEnd,
		Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "call-b", Result: results[1]}})
	// call-a is still pending: its create card must survive, while call-b's
	// replacement already removed its own clear/create pair.
	if got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n")); strings.Count(got, "Goal created rev") != 1 || strings.Contains(got, "Goal cleared") {
		t.Fatalf("out-of-order finish removed the wrong cards:\n%s", got)
	}
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionEnd,
		Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "call-a", Result: results[0]}})
	got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n"))
	for _, leaked := range []string{"Goal created rev", "Goal replaced rev", "Goal cleared"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("concurrent objective calls left %q:\n%s", leaked, got)
		}
	}
	if len(renderer.Chat.Children) != 2 {
		t.Fatalf("children = %d, want two tool cards:\n%s", len(renderer.Chat.Children), got)
	}
}

// A durable card inserted before the streaming component is still removable:
// the coalescer keeps the component identity, not a transcript index.
func TestGoalCoalescingKeepsStreamingComponent(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.EntryRenderer = GoalEntryRenderer
	streaming := &fakeStreamingComponent{}
	renderer.StreamingComponent = streaming
	renderer.Chat.AddChild(streaming)

	renderer.goalActions.start("set_goal", "call", json.RawMessage(`{"objective":"ship it"}`))
	renderer.AddCustomEntryToChat(&coding.SessionEntry{Type: "custom", CustomType: "pi-goal",
		Data: json.RawMessage(`{"operation":"create","goal":{"id":"g","revision":1,"phase":"active","objective":"ship it"}}`)})
	if len(renderer.Chat.Children) != 2 {
		t.Fatalf("children = %d", len(renderer.Chat.Children))
	}
	renderer.goalActions.finish("call", &SortToolResultContent{
		Details: json.RawMessage(`{"goal":{"id":"g","revision":1,"phase":"active"}}`),
		Content: []ToolResultContent{{Type: "text", Text: "Goal created (id g, revision 1)."}},
	}, renderer.Chat)
	if len(renderer.Chat.Children) != 1 || renderer.Chat.Children[0] != streaming {
		t.Fatalf("transcript = %d children", len(renderer.Chat.Children))
	}
}

type fakeStreamingComponent struct{}

func (*fakeStreamingComponent) Render(int) []string { return []string{"streaming"} }
func (*fakeStreamingComponent) Invalidate()         {}
