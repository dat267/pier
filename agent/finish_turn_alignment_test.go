package agent

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-loop.ts lets finishTurn request one provider turn with unchanged context.
func TestFinishTurnCanRequestContextOnlyContinuation(t *testing.T) {
	responses := []*ai.AssistantMessage{
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "first"}}, ai.StopStop),
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "second"}}, ai.StopStop),
	}
	streamCalls := 0
	streamFn := func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		response := responses[streamCalls]
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: response.StopReason, Message: response})
		return stream
	}
	var timeline []string
	finishCalls := 0
	config := &AgentLoopConfig{
		Model: createModel(), ConvertToLlm: identityConverter,
		FinishTurn: func(turn *AgentTurnContext, _ context.Context) (*AgentTurnDecision, error) {
			finishCalls++
			if turn.Message == nil || len(turn.NewMessages) == 0 {
				t.Fatalf("incomplete turn context: %+v", turn)
			}
			timeline = append(timeline, "finish")
			if finishCalls == 1 {
				return &AgentTurnDecision{Action: AgentTurnContinue}, nil
			}
			return &AgentTurnDecision{Action: AgentTurnEnd}, nil
		},
	}
	messages := RunAgentLoop([]ai.Message{createUserMessage("go")}, AgentContext{}, config, context.Background(), func(event AgentEvent) error {
		timeline = append(timeline, event.Type)
		return nil
	}, streamFn)
	if finishCalls != 2 || streamCalls != 2 {
		t.Fatalf("finish/stream calls = %d/%d, want 2/2", finishCalls, streamCalls)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %v, want prompt and two assistant responses", messageRoles(messages))
	}
	finishIndex, turnEndCount := -1, 0
	for i, event := range timeline {
		if event == "finish" {
			finishIndex = i
		}
		if event == TurnEnd {
			turnEndCount++
			if finishIndex < 0 {
				t.Fatalf("turn_end emitted before finishTurn: %v", timeline)
			}
			finishIndex = -1
		}
	}
	if turnEndCount != 2 {
		t.Fatalf("turn_end count = %d, want 2; events %v", turnEndCount, timeline)
	}
}

// Upstream agent-loop.ts calls finishTurn for errors and aborts before turn_end, then exits.
func TestFinishTurnRunsForErrorResponseBeforeTurnEnd(t *testing.T) {
	response := createAssistantMessage(nil, ai.StopError)
	streamFn, calls := mockStreamFn(response)
	var timeline []string
	config := &AgentLoopConfig{
		Model: createModel(), ConvertToLlm: identityConverter,
		FinishTurn: func(*AgentTurnContext, context.Context) (*AgentTurnDecision, error) {
			timeline = append(timeline, "finish")
			return &AgentTurnDecision{Action: AgentTurnContinue}, nil
		},
	}
	RunAgentLoop([]ai.Message{createUserMessage("go")}, AgentContext{}, config, context.Background(), func(event AgentEvent) error {
		timeline = append(timeline, event.Type)
		return nil
	}, streamFn)
	finishIndex, turnEndIndex := -1, -1
	for i, event := range timeline {
		if event == "finish" {
			finishIndex = i
		}
		if event == TurnEnd {
			turnEndIndex = i
		}
	}
	if finishIndex < 0 || turnEndIndex <= finishIndex {
		t.Fatalf("finishTurn must precede turn_end on error: %v", timeline)
	}
	if *calls != 1 {
		t.Fatalf("error response continued despite finish decision: stream calls = %d", *calls)
	}
}
