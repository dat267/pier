package coding

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// A queued follow-up waits for the goal: the goal's rounds run to completion,
// the agent answers them, and only then does the follow-up become its own run.
func TestGoalHoldsFollowUpsUntilItsFinalAnswer(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	var prompts []string
	round := 0
	streamFn := func(m *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		var text strings.Builder
		for _, message := range transcript.Messages {
			text.WriteString(extractTextContent(message))
			text.WriteString("\n")
		}
		prompts = append(prompts, text.String())
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
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after the goal"}})
	session.Goal.HandleCommand("set objective")
	if len(pending) != 1 {
		t.Fatalf("round reservations = %d, want 1", len(pending))
	}

	// Both rounds run to completion: the second one completes the goal, so the
	// follow-up is released as its own run afterwards.
	if err := pending[0](context.Background()); err != nil {
		t.Fatal(err)
	}
	if goalState := session.Goal.Snapshot().Goal; goalState == nil || goalState.Phase != goal.PhaseComplete {
		t.Fatalf("goal = %+v, want complete", goalState)
	}
	if len(prompts) != 2 {
		t.Fatalf("model runs = %d, want the two goal rounds", len(prompts))
	}
	for _, prompt := range prompts {
		if strings.Contains(prompt, "after the goal") {
			t.Fatalf("follow-up ran before the goal finished: %q", prompt)
		}
	}
	if len(pending) != 2 {
		t.Fatalf("work after completion = %d, want the released follow-up run", len(pending))
	}
	if err := pending[1](context.Background()); err != nil {
		t.Fatal(err)
	}
	if queued := session.GetFollowUpMessages(); len(queued) != 0 {
		t.Fatalf("follow-ups after the released run = %v, want delivered", queued)
	}
	last := prompts[len(prompts)-1]
	if !strings.Contains(last, "after the goal") {
		t.Fatalf("last run prompt = %q, want the released follow-up", last)
	}
}
