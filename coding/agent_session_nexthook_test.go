package coding

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// transcriptText flattens the messages a stream call received, so a test can
// assert what reached the provider rather than reaching into the session.
func transcriptText(messages []ai.Message) string {
	var parts []string
	for _, message := range messages {
		switch typed := message.(type) {
		case *ai.SystemMessage:
			parts = append(parts, ai.ContentText(typed.Content, "\n"))
		case *ai.UserMessage:
			parts = append(parts, ai.ContentText(typed.Content, "\n"))
		case *ai.AssistantMessage:
			for _, block := range typed.Content {
				if text, ok := block.(ai.TextContent); ok {
					parts = append(parts, text.Text)
				}
			}
		case *ai.ToolResultMessage:
			for _, block := range typed.Content {
				if text, ok := block.(ai.TextContent); ok {
					parts = append(parts, text.Text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

// TestSessionChainsCallerNextTurnHook pins upstream's chaining rule
// (agent-session.ts _installAgentNextTurnRefresh captures the hook the Agent
// already carried). The port assigned its own hook over the caller's, so a
// session-configured hook never ran.
func TestSessionChainsCallerNextTurnHook(t *testing.T) {
	model := &ai.Model{ID: "only", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}

	var mu sync.Mutex
	var transcripts []string
	var calls int
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		mu.Lock()
		calls++
		call := calls
		transcripts = append(transcripts, transcriptText(transcript.Messages))
		mu.Unlock()

		stream := ai.NewAssistantMessageEventStream()
		go func() {
			if call == 1 {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopToolUse, Message: &ai.AssistantMessage{
					API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
					Content:    ai.ContentList{ai.ToolCall{ID: "t1", Name: "read", Arguments: json.RawMessage(`{}`)}},
					StopReason: ai.StopToolUse,
				}})
				return
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}

	hookRuns := 0
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{testTool("read")},
		Goal:  false,
		PrepareNextTurnWithContext: func(turn *agent.AgentTurnContext, ctx context.Context) (*agent.AgentLoopTurnUpdate, error) {
			hookRuns++
			note := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hook note"}, Timestamp: 1}
			return &agent.AgentLoopTurnUpdate{Messages: []ai.Message{note}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := session.PromptText(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}

	if hookRuns == 0 {
		t.Fatal("the session dropped the caller's prepare-next-turn hook")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(transcripts) != 2 {
		t.Fatalf("stream calls = %d; want 2", len(transcripts))
	}
	second := transcripts[1]
	if !strings.Contains(second, "hook note") {
		t.Fatalf("second request missing the hook's message: %q", second)
	}
	// The session's own refresh still runs: the tool result stays in context.
	if !strings.Contains(second, "go") {
		t.Fatalf("second request lost the conversation: %q", second)
	}
}

// TestSessionNextTurnHookMergesLikeUpstream pins the merge rules of upstream's
// _installAgentNextTurnRefresh: the caller's context replaces the turn context
// (including its messages), but the session's state still supplies the model,
// the thinking level and the tools.
func TestSessionNextTurnHookMergesLikeUpstream(t *testing.T) {
	sessionModel := &ai.Model{ID: "session-model", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	hookModel := &ai.Model{ID: "hook-model", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}

	var mu sync.Mutex
	var transcripts []string
	var models []string
	var calls int
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		mu.Lock()
		calls++
		call := calls
		transcripts = append(transcripts, transcriptText(transcript.Messages))
		models = append(models, requestModel.ID)
		mu.Unlock()

		stream := ai.NewAssistantMessageEventStream()
		go func() {
			if call == 1 {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopToolUse, Message: &ai.AssistantMessage{
					API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
					Content:    ai.ContentList{ai.ToolCall{ID: "t1", Name: "read", Arguments: json.RawMessage(`{}`)}},
					StopReason: ai.StopToolUse,
				}})
				return
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}

	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: sessionModel, StreamFn: streamFn,
		Tools: []agent.AgentTool{testTool("read")},
		PrepareNextTurnWithContext: func(turn *agent.AgentTurnContext, ctx context.Context) (*agent.AgentLoopTurnUpdate, error) {
			return &agent.AgentLoopTurnUpdate{
				Context: &agent.AgentContext{
					Messages: []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hook context"}, Timestamp: 2}},
					Tools:    nil,
				},
				Model: hookModel,
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := session.PromptText(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(models) != 2 {
		t.Fatalf("stream calls = %v; want 2", models)
	}
	if models[1] != "session-model" {
		t.Fatalf("request model = %q; the session's state must win over the hook's model", models[1])
	}
	second := transcripts[1]
	if !strings.Contains(second, "hook context") || strings.Contains(second, "go") {
		t.Fatalf("the hook's context replacement was ignored: %q", second)
	}
}
