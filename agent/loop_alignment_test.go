package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-loop.ts runs prepareRequest before every provider request, including the first.
func TestPrepareRequestRunsBeforeEveryProviderCall(t *testing.T) {
	toolCall := createAssistantMessage(ai.ContentList{
		ai.ToolCall{ID: "missing", Name: "missing", Arguments: []byte(`{}`)},
	}, ai.StopToolUse)
	final := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "done"}}, ai.StopStop)
	var streamCalls int
	var models []string
	var reasoning []ai.ThinkingLevel
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		models = append(models, model.ID)
		reasoning = append(reasoning, options.Reasoning)
		response := toolCall
		if streamCalls > 0 {
			response = final
		}
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: response.StopReason, Message: response})
		return stream
	}
	preparedModel := *createModel()
	preparedModel.ID = "prepared"
	prepareCalls := 0
	config := &AgentLoopConfig{
		Model: createModel(), Reasoning: ai.ThinkLow, ConvertToLlm: identityConverter,
		PrepareRequest: func(request *PrepareRequestContext, _ context.Context) (*AgentRequestUpdate, error) {
			prepareCalls++
			if prepareCalls == 1 {
				if len(request.Context.Messages) != 1 || ai.RoleOf(request.Context.Messages[0]) != ai.RoleUser {
					return nil, fmt.Errorf("first request context = %v", messageRoles(request.Context.Messages))
				}
				updated := request.Context
				updated.Messages = append(append([]ai.Message{}, updated.Messages...), createUserMessage("prepared context"))
				return &AgentRequestUpdate{
					Context: &updated, Model: &preparedModel,
					ThinkingLevel: ai.ThinkHigh, HasThinkingLevel: true,
				}, nil
			}
			if request.Model.ID != "prepared" || request.ThinkingLevel != ai.ThinkHigh {
				return nil, fmt.Errorf("second request model/thinking = %s/%s", request.Model.ID, request.ThinkingLevel)
			}
			if len(request.Context.Messages) < 4 {
				return nil, fmt.Errorf("second request context = %v", messageRoles(request.Context.Messages))
			}
			prepared, ok := request.Context.Messages[1].(*ai.UserMessage)
			if !ok || prepared.Content.Text != "prepared context" {
				return nil, fmt.Errorf("prepared context message = %#v", request.Context.Messages[1])
			}
			return nil, nil
		},
	}
	stream := AgentLoop([]ai.Message{createUserMessage("go")}, AgentContext{}, config, nil, streamFn)
	drainAgentStream(t, stream)
	if prepareCalls != 2 || streamCalls != 2 {
		t.Fatalf("prepare/stream calls = %d/%d, want 2/2", prepareCalls, streamCalls)
	}
	for i := range models {
		if models[i] != "prepared" || reasoning[i] != ai.ThinkHigh {
			t.Fatalf("request %d model/thinking = %s/%s", i, models[i], reasoning[i])
		}
	}
}

func TestPrepareNextTurnErrorFailsRun(t *testing.T) {
	toolCall := createAssistantMessage(ai.ContentList{
		ai.ToolCall{ID: "missing", Name: "missing", Arguments: []byte(`{}`)},
	}, ai.StopToolUse)
	streamFn, _ := mockStreamFn(
		toolCall,
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "must not run"}}, ai.StopStop),
	)
	wantErr := errors.New("prepare failed")
	a, err := NewAgent(&AgentOptions{
		StreamFn: streamFn,
		PrepareNextTurn: func(context.Context) (*AgentLoopTurnUpdate, error) {
			return nil, wantErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if got := a.State().ErrorMessage; got != wantErr.Error() {
		t.Fatalf("ErrorMessage = %q, want %q", got, wantErr)
	}
	if got := a.State().Messages[len(a.State().Messages)-1].(*ai.AssistantMessage).StopReason; got != ai.StopError {
		t.Fatalf("failure stop reason = %q, want %q", got, ai.StopError)
	}
}
