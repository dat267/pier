package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// D223: active goals expose get_goal before their model round starts. The
// extension's nonblocking sendMessage leaves time for syncGoalTools before
// provider dispatch; SDK idle delivery here can start a model run inline.
func TestIdleGoalRoundReceivesActiveGoalTools(t *testing.T) {
	for _, commandStartsRound := range []bool{true, false} {
		name := "idle-command"
		if !commandStartsRound {
			name = "tool-create-control"
		}
		t.Run(name, func(t *testing.T) {
			manager, dir := newTestSession(t)
			model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
			var session *AgentSession
			calls := 0
			sawGetGoal := false
			streamFn := func(model *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				calls++
				for _, tool := range ai.GetCurrentTools(transcript.Messages) {
					if tool.Name == "get_goal" {
						sawGetGoal = true
					}
				}
				_, _ = session.GetToolDefinition("set_goal").Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), nil)
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
			if commandStartsRound {
				session.Goal.HandleCommand("set finish the objective")
			} else {
				if _, err := session.GetToolDefinition("set_goal").Execute("create", json.RawMessage(`{"objective":"finish the objective"}`), context.Background(), nil); err != nil {
					t.Fatal(err)
				}
				if err := session.Prompt(context.Background(), "continue", nil); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("model calls = %d, want 1", calls)
			}
			if !sawGetGoal {
				t.Fatal("active goal's first model request did not expose get_goal")
			}
		})
	}
}
