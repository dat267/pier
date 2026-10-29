package coding

// A per-turn reminder must ride the transcript, never the system prompt.
// jcode hit this directly: injecting a swarm notification into the system
// prompt changed the prefix of every cached message, so the reminder turn and
// the turn after it both cost a full prompt-cache miss
// (crates/jcode-app-core/src/agent_tests/system_prompt_reminder.rs). Pier's
// goal notices are transcript messages for the same reason; this pins the
// invariant so a prompt rebuild cannot quietly reintroduce the miss.

import (
	"context"
	"sync"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

func newStablePromptSession(t *testing.T, systemPrompts *[]string, mu *sync.Mutex) *AgentSession {
	t.Helper()
	tempAgentDir(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session, err := NewAgentSession(&SessionConfig{
		Cwd:      t.TempDir(),
		Model:    model,
		Tools:    []agent.AgentTool{testTool("read")},
		StreamFn: captureSystemPromptFn(systemPrompts, mu),
	})
	if err != nil {
		t.Fatalf("NewAgentSession: %v", err)
	}
	session.control = &AgentSessionControl{Tools: map[string]AgentToolDefinition{
		"read": {Tool: testTool("read"), PromptGuidelines: []string{"read carefully"}},
	}}
	return session
}

func captureSystemPromptFn(prompts *[]string, mu *sync.Mutex) StreamFnFn {
	return func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		mu.Lock()
		*prompts = append(*prompts, ai.GetCurrentSystemPrompt(transcript.Messages))
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
}

func TestSystemPromptIsStableAcrossTurns(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	session := newStablePromptSession(t, &prompts, &mu)
	ctx := context.Background()

	if err := session.Prompt(ctx, "first", nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	// The reminder turn: a transcript message the model sees, of the kind the
	// goal controller delivers between rounds.
	reminder, err := ai.MarshalJSON(map[string]any{"objective": "keep going"})
	if err != nil {
		t.Fatalf("marshal reminder: %v", err)
	}
	session.Sessions.AppendMessage(&ai.CustomMessage{Role: "pi-goal-turn", Content: reminder})
	if err := session.Prompt(ctx, "second", nil); err != nil {
		t.Fatalf("reminder turn: %v", err)
	}
	if err := session.Prompt(ctx, "third", nil); err != nil {
		t.Fatalf("follow-up turn: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 3 {
		t.Fatalf("captured %d prompts, want 3", len(prompts))
	}
	if prompts[0] == "" {
		t.Fatal("the first turn carried an empty system prompt")
	}
	for i, prompt := range prompts {
		if prompt != prompts[0] {
			t.Fatalf("turn %d system prompt differs from the first:\n%q\n%q", i+1, prompts[0], prompt)
		}
	}
}
