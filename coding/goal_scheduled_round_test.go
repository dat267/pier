package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// D223's goal state machine stops future rounds on pause/clear/replace. Idle
// round scheduling is Go-only (D143), so a reserved round must still be current
// when the host starts its deferred work. No callback runs under a goal lock.
func TestGoalScheduledRoundSkipsInvalidatedReservation(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		commands []string
		want     int
	}{
		{"cleared", []string{"clear"}, 0},
		{"paused", []string{"pause"}, 0},
		{"replaced", []string{"set new objective"}, 1},
		{"paused-and-resumed", []string{"pause", "resume"}, 1},
		{"current-control", nil, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			manager, dir := newTestSession(t)
			model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
			var session *AgentSession
			calls := 0
			streamFn := func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				calls++
				// A valid reserved round completes its goal, preventing another
				// round from hiding an unexpected extra admission in this test.
				if snapshot := session.Goal.Snapshot(); snapshot.Goal != nil && snapshot.Armed {
					_, _ = session.GetToolDefinition("set_goal").Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), nil)
				}
				stream := ai.NewAssistantMessageEventStream()
				go func() {
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
						API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
						Content: ai.ContentList{ai.TextContent{Text: "done"}},
					}})
				}()
				return stream
			}
			var err error
			session, err = NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm})
			if err != nil {
				t.Fatal(err)
			}
			var pending []func(context.Context) error
			session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
			session.Goal.HandleCommand("set old objective")
			if len(pending) != 1 {
				t.Fatalf("initial round reservations = %d, want 1", len(pending))
			}
			for _, command := range fixture.commands {
				session.Goal.HandleCommand(command)
			}
			for _, work := range pending {
				if err := work(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if calls != fixture.want {
				t.Fatalf("model runs = %d, want %d after %v", calls, fixture.want, fixture.commands)
			}
		})
	}
}
