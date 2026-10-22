package coding

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Reference: pi v1.3.0 core/compaction/compaction.ts
// createSummarizationOptions forwards provider environment for both summaries.
func TestAuditCompactionForwardsProviderEnvironment(t *testing.T) {
	preparation := &CompactionPreparation{
		FirstKeptEntryID: "kept", FileOps: CreateFileOps(),
		MessagesToSummarize: []ai.Message{createUserMessage("history")},
		Settings:            DefaultCompactionSettings,
	}
	var seen ai.ProviderEnv
	_, err := Compact(preparation, CompactionOptions{
		Model: &ai.Model{ID: "m", API: ai.APIOpenAIResponses, Provider: "openai", MaxTokens: 8192},
		Env:   map[string]string{"AWS_REGION": "test-region"},
		StreamFn: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			seen = options.Env
			stream := ai.NewAssistantMessageEventStream()
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "summary"}}, StopReason: ai.StopStop,
			}})
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen["AWS_REGION"] != "test-region" {
		t.Fatalf("summary request environment = %v, want configured AWS_REGION", seen)
	}
}

// Reference: pi v1.3.0 core/agent-session.ts
// _compactBeforeNextAssistantResponse estimates the complete projection,
// including tool results since the previous provider usage.
func TestAuditCompactionCountsNewToolResults(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	var calls int
	var summaryCalls atomic.Int64
	var summariesBeforeSecondRequest int64
	streamFn := func(m *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		message := &ai.AssistantMessage{
			API: m.API, Provider: m.Provider, Model: m.ID, Timestamp: timeNowMS(),
			Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
			Usage: ai.Usage{Input: 600, Output: 10, TotalTokens: 610},
		}
		if calls == 1 {
			message.Content = ai.ContentList{ai.ToolCall{ID: "large-call", Name: "large", Arguments: json.RawMessage(`{}`)}}
			message.StopReason = ai.StopToolUse
		} else {
			summariesBeforeSecondRequest = summaryCalls.Load()
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		return stream
	}
	tool := testTool("large")
	tool.Execute = func(_ string, _ json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
		return agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: strings.Repeat("x", 3200)}}}, nil
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: streamFn,
		Control:  &AgentSessionControl{autoCompaction: true},
		Settings: SessionSettings{Compaction: smallCompactionSettings()}, Tools: []agent.AgentTool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedBranch(t, session)
	session.CompactionStreamFn = summarizingStreamFn(t, &summaryCalls, "short summary")
	if err := session.PromptText(context.Background(), "fetch large output"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("assistant requests = %d, want 2", calls)
	}
	if summariesBeforeSecondRequest == 0 {
		t.Fatal("next assistant request sent without compacting 610 usage tokens plus an 800-token tool result in a 1000-token window")
	}
}

// Reference: pi v1.3.0 core/agent-session.ts _getSummarizationRequestAuth
// and _runDefaultCompaction carry the selected thinking level into compaction.
func TestAuditCompactionKeepsSelectedReasoning(t *testing.T) {
	for _, manual := range []bool{true, false} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			var runs atomic.Int64
			model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", Reasoning: true, ContextWindow: 100000, MaxTokens: 8192}
			session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary"), smallCompactionSettings())
			seedBranch(t, session)
			session.SetThinkingLevel(ai.ThinkHigh)
			var seen ai.ThinkingLevel
			stream := summarizingStreamFn(t, &runs, "summary")
			session.CompactionStreamFn = func(m *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				seen = options.Reasoning
				return stream(m, transcript, options)
			}
			var err error
			if manual {
				_, err = session.CompactSession(context.Background(), "")
			} else {
				_, err = session.RunAutoCompaction(context.Background(), CompactionThreshold, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if seen != ai.ThinkHigh {
				t.Fatalf("summary reasoning = %q, want high", seen)
			}
		})
	}
}

// Reference: pi v1.3.0 core/agent-session.ts _findPersistedMessageEntryId
// falls back to persisted/projection provenance for resumed messages.
func TestAuditCompactionRecoversResumedOverflow(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	var runs atomic.Int64
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary"), smallCompactionSettings())
	seedBranch(t, session)
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: model.API, Provider: model.Provider, Model: model.ID, Timestamp: timeNowMS(),
		StopReason: ai.StopError, ErrorMessage: strPtr("prompt is too long: 2000 tokens > 1000 maximum"),
	})
	// Match SDK resume: load persisted messages directly, without message_end events.
	session.Agent.SetMessages(session.Sessions.Projection().Messages)
	if err := session.PromptText(context.Background(), "continue after resume"); err != nil {
		t.Fatalf("resumed overflow blocks next prompt: %v", err)
	}
}

// Reference: pi v1.3.0 core/compaction/compaction.ts
// estimateProjectedContextTokens compares usage provenance in branch order,
// not the compaction window's replay order.
func TestAuditCompactionInvalidatesRetainedUsage(t *testing.T) {
	manager := newTestSessionManager(t)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "old question"}, Timestamp: 1})
	kept := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "recent question"}, Timestamp: 2})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "recent answer"}},
		API:     ai.APIOpenAIResponses, Provider: "openai", Model: "m", StopReason: ai.StopStop, Timestamp: 3,
		Usage: ai.Usage{Input: 900000, Output: 100, TotalTokens: 900100},
	})
	manager.AppendCompaction("short summary", kept, 900100, nil, false, nil)
	estimate := manager.ProjectedContextEstimate()
	if estimate.UsageTokens != 0 || estimate.Tokens > 1000 {
		t.Fatalf("post-compaction estimate trusts retained pre-compaction usage: %+v", estimate)
	}
}
