package agent

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf), core/agent-session.ts prepareRequest routes a virtual
// selection per request: the routed physical model and thinking level apply to
// that request only, and the run keeps its own selection so the next request is
// routed again. AgentRequestUpdate.RequestModel is that request-scoped shape.
func TestPrepareRequestRequestScopedModel(t *testing.T) {
	var streamedModels []string
	var streamedLevels []ai.ThinkingLevel
	streamCalls := 0
	streamFn := func(model *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		streamedModels = append(streamedModels, model.ID)
		streamedLevels = append(streamedLevels, options.Reasoning)
		response := createAssistantMessage(ai.ContentList{
			ai.ToolCall{ID: "missing", Name: "missing", Arguments: []byte(`{}`)},
		}, ai.StopToolUse)
		if streamCalls > 0 {
			response = createAssistantMessage(ai.ContentList{ai.TextContent{Text: "done"}}, ai.StopStop)
		}
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: response.StopReason, Message: response})
		return stream
	}
	physical := *createModel()
	physical.ID = "physical"
	selection := createModel()
	selection.ID = "virtual"
	var seenModels []string
	config := &AgentLoopConfig{
		Model: selection, Reasoning: ai.ThinkLow, ConvertToLlm: defaultConvertToLlm,
		PrepareRequest: func(_ context.Context, request *PrepareRequestContext) (*AgentRequestUpdate, error) {
			seenModels = append(seenModels, request.Model.ID)
			return &AgentRequestUpdate{
				RequestModel:            &physical,
				RequestThinkingLevel:    ai.ThinkHigh,
				HasRequestThinkingLevel: true,
			}, nil
		},
	}
	stream := AgentLoop(nil, []ai.Message{createUserMessage("go")}, AgentContext{}, config, streamFn)
	drainAgentStream(t, stream)

	if streamCalls != 2 || len(streamedModels) != 2 {
		t.Fatalf("stream calls = %d, models = %v", streamCalls, streamedModels)
	}
	for index := range streamedModels {
		if streamedModels[index] != "physical" {
			t.Fatalf("request %d streamed %q, want the routed physical model", index, streamedModels[index])
		}
		if streamedLevels[index] != ai.ThinkHigh {
			t.Fatalf("request %d level = %q, want the routed level", index, streamedLevels[index])
		}
	}
	// The run kept its own selection, so the hook sees the virtual model again.
	for index, model := range seenModels {
		if model != "virtual" {
			t.Fatalf("prepare %d saw %q, want the run's selection", index, model)
		}
	}
	if config.Model.ID != "virtual" || config.Reasoning != ai.ThinkLow {
		t.Fatalf("run selection changed to %s/%q", config.Model.ID, config.Reasoning)
	}
}
