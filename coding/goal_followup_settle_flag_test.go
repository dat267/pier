package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// A settle that leaves a released follow-up run behind says so, so the working
// period stays open across the release instead of closing and restarting
// (D402). The released run's own settle reports nothing pending.
func TestGoalSettleReportsThePendingFollowUpRun(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	var settled []bool
	round := 0
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		round++
		if round == 2 {
			_, _ = session.GetToolDefinition("set_goal").Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), nil)
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "done"}},
			}})
		}()
		return stream
	}
	var err error
	session, err = NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionAgentSettled {
			settled = append(settled, event.FollowUpsPending)
		}
	})
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after the goal"}})
	session.Goal.HandleCommand("set objective")
	for index := 0; index < len(pending); index++ {
		if err := pending[index](context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(settled) != 2 {
		t.Fatalf("settled events = %v, want the goal chain and the released run", settled)
	}
	if !settled[0] {
		t.Error("the goal chain's settle did not report the released follow-up run")
	}
	if settled[1] {
		t.Error("the released run's settle still reported pending follow-ups")
	}
}
