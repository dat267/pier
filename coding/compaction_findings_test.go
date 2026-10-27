package coding

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf16"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Promoted from coding/testdata/compaction-audit (finding 3). Reference: pi
// v1.4.0 core/compaction/compaction.ts estimateProjectedContextTokens compares
// usage provenance in branch order, not the compaction window's replay order.
func TestCompactionInvalidatesRetainedUsage(t *testing.T) {
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

// Promoted from coding/testdata/compaction-audit (finding 1). Reference: pi
// v1.4.0 core/agent-session.ts _findPersistedMessageEntryId falls back to
// persisted/projection provenance for resumed messages.
func TestCompactionRecoversResumedOverflow(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	var runs atomic.Int64
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary"), smallCompactionSettings())
	seedBranch(t, session)
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: model.API, Provider: model.Provider, Model: model.ID, Timestamp: timeNowMS(),
		StopReason: ai.StopError, ErrorMessage: new("prompt is too long: 2000 tokens > 1000 maximum"),
	})
	// Match SDK resume: load persisted messages directly, without message_end events.
	session.Agent.SetMessages(session.Sessions.Projection().Messages)
	if err := session.PromptText(context.Background(), "continue after resume"); err != nil {
		t.Fatalf("resumed overflow blocks next prompt: %v", err)
	}
}

// Promoted from coding/testdata/compaction-audit (finding 2). Reference: pi
// v1.4.0 core/agent-session.ts _compactBeforeNextAssistantResponse estimates
// the complete projection, including tool results since the previous provider
// usage.
func TestCompactionCountsNewToolResults(t *testing.T) {
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

// Promoted from coding/testdata/compaction-audit (finding 6). Reference: pi
// v1.4.0 core/agent-session.ts _omitRecoveryAttempt refreshes finalized
// context immediately, even if the subsequent compaction fails.
func TestCompactionFailureKeepsLiveContextProjected(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	base := erroringStreamFn(1, "prompt is too long: 2000 tokens > 1000 maximum")
	calls := 0
	replayedOmission := false
	session := compactionTestSession(t, model, func(m *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		if calls == 2 {
			for _, message := range transcript.Messages {
				if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopError {
					replayedOmission = true
				}
			}
		}
		return base(m, transcript, options)
	}, smallCompactionSettings())
	seedBranch(t, session)
	session.CompactionStreamFn = erroringStreamFn(1, "summarizer unavailable")
	err := session.PromptText(context.Background(), "continue the task")
	if err == nil || !strings.Contains(err.Error(), "summarizer unavailable") {
		t.Fatalf("expected the summary failure, got %v", err)
	}
	for _, message := range session.Sessions.Projection().Messages {
		if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopError {
			t.Fatal("reproduction did not persist the overflow omission")
		}
	}
	if err := session.PromptText(context.Background(), "continue after compaction failure"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("assistant requests = %d, want 2", calls)
	}
	if replayedOmission {
		t.Fatal("the next provider request replayed the persistently omitted overflow response")
	}
}

// Promoted from coding/testdata/compaction-audit (finding 4). Reference: pi
// v1.4.0 core/compaction/compaction.ts createSummarizationOptions forwards
// provider environment for both summaries.
func TestCompactionForwardsProviderEnvironment(t *testing.T) {
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

// Promoted from coding/testdata/compaction-audit (finding 5). Reference: pi
// v1.4.0 core/agent-session.ts _getSummarizationRequestAuth and
// _runDefaultCompaction carry the selected thinking level into compaction.
func TestCompactionKeepsSelectedReasoning(t *testing.T) {
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

// Promoted from coding/testdata/compaction-audit (finding 7). Reference: pi
// v1.4.0 core/agent-session.ts _getSummarizationRequestAuth passes the
// compaction signal into auth resolution in both compaction paths.
func TestCompactionAuthReceivesRequestContext(t *testing.T) {
	for _, manual := range []bool{true, false} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observe := false
			cancelReachedAuth := false
			provider := stubProvider("alpha")
			provider.Auth.APIKey.Resolve = func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
				if observe {
					cancel()
					cancelReachedAuth = input.Ctx2.Err() != nil
				}
				return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "test-key"}}, nil
			}
			runtime := runtimeWithProviders(t, provider)
			model := runtime.GetModel("alpha", "alpha-model")
			var runs atomic.Int64
			session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary"), smallCompactionSettings())
			session.control.ModelRuntime = runtime
			seedBranch(t, session)
			observe = true
			var err error
			if manual {
				_, err = session.CompactSession(ctx, "")
			} else {
				_, err = session.RunAutoCompaction(ctx, CompactionThreshold, false)
			}
			if err == nil {
				t.Fatal("cancelled compaction did not report cancellation")
			}
			if !cancelReachedAuth {
				t.Fatal("cancelling compaction did not cancel its auth resolution context")
			}
		})
	}
}

// Promoted from coding/testdata/compaction-audit (finding 8). Reference: pi
// v1.4.0 core/compaction/utils.ts computeFileLists uses JavaScript's default
// UTF-16 code-unit sort, not UTF-8 byte order.
func TestCompactionFileListsUseUTF16Order(t *testing.T) {
	ops := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.AssistantMessage{Content: ai.ContentList{
		ai.ToolCall{ID: "a", Name: "read", Arguments: json.RawMessage(`{"path":"\ue000.txt"}`)},
		ai.ToolCall{ID: "b", Name: "read", Arguments: json.RawMessage(`{"path":"😀.txt"}`)},
	}}, ops)
	read, _ := ComputeFileLists(ops)
	if got := strings.Join(read, ","); got != "😀.txt,\ue000.txt" {
		t.Fatalf("read-file order = %q, want UTF-16 order %q", got, "😀.txt,\ue000.txt")
	}
}

// TestCompareUTF16MatchesEncodedUnits pins compareUTF16 and sortStrings to
// the reference UTF-16 code-unit ordering over shapes where UTF-8 order
// differs: supplementary scalars against high BMP scalars, prefixes, NULs,
// and mixed scripts.
func TestCompareUTF16MatchesEncodedUnits(t *testing.T) {
	values := []string{
		"\ue000.txt", "😀.txt", "\uffff", "𐀀", "a", "aa", "ab",
		"a\x00b", "z", "Z", "😀", "😀\x00", "é", "e",
		"한자", "한", "\ufffd", "a\ufffd", "ascii", "😀😀",
	}
	reference := func(a, b string) int {
		unitsA, unitsB := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
		for index := 0; index < len(unitsA) && index < len(unitsB); index++ {
			if unitsA[index] != unitsB[index] {
				return int(unitsA[index]) - int(unitsB[index])
			}
		}
		return len(unitsA) - len(unitsB)
	}
	for _, a := range values {
		for _, b := range values {
			got := compareUTF16(a, b)
			want := reference(a, b)
			if (got < 0) != (want < 0) || (got > 0) != (want > 0) || (got == 0) != (want == 0) {
				t.Fatalf("compareUTF16(%q, %q) = %d, reference %d", a, b, got, want)
			}
		}
	}
	sorted := append([]string{}, values...)
	sortStrings(sorted)
	expected := append([]string{}, values...)
	sort.Slice(expected, func(i, j int) bool { return reference(expected[i], expected[j]) < 0 })
	for index := range sorted {
		if sorted[index] != expected[index] {
			t.Fatalf("sortStrings order = %q, want %q", sorted, expected)
		}
	}
}

// Reference: pi v1.4.0 02726086d core/agent-session.ts _omitRecoveryAttempt
// (issue #11227). Recovery of a length stop that a resumed session persisted
// along with the tool results answering its calls omitted only the assistant
// message, so the orphaned results reached the next request and providers
// rejected it: "Messages with role 'tool' must be a response to a preceding
// message with 'tool_calls'".
func TestRecoveryOmitsToolResultsWithTheirToolCall(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIOpenAICompletions, Provider: "openai", ContextWindow: 1000, MaxTokens: 100}
	var requests [][]ai.Message
	streamFn := func(m *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		requests = append(requests, append([]ai.Message(nil), transcript.Messages...))
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
			API: m.API, Provider: m.Provider, Model: m.ID,
			Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
		}})
		return stream
	}
	session := compactionTestSession(t, model, streamFn, smallCompactionSettings())
	seedBranch(t, session)

	// A truncated response and its tool result, as a resumed session finds them.
	truncated := &ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: "openai", Model: "m",
		Content: ai.ContentList{
			ai.TextContent{Text: "checking"},
			ai.ToolCall{ID: "call_truncated", Name: "bash", Arguments: json.RawMessage(`{"command":"echo"}`)},
		},
		StopReason: ai.StopLength,
		Usage:      ai.Usage{Input: 500, Output: 16, TotalTokens: 516},
	}
	result := &ai.ToolResultMessage{
		ToolCallID: "call_truncated", ToolName: "bash", IsError: true,
		Content: ai.UserContentList{ai.TextContent{Text: "Not executed: output token limit reached."}},
	}
	session.Sessions.AppendMessage(truncated)
	session.Sessions.AppendMessage(result)
	projection := session.Sessions.Projection()
	session.Agent.SetMessages(projection.Messages)
	session.refreshFinalizedContext(projection)

	summaryCalls := atomic.Int64{}
	session.CompactionStreamFn = summarizingStreamFn(t, &summaryCalls, "summary")
	if err := session.PromptText(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if summaryCalls.Load() == 0 {
		t.Fatal("pre-prompt recovery did not compact")
	}
	if len(requests) == 0 {
		t.Fatal("no provider request reached the model")
	}
	for _, message := range requests[0] {
		if ai.RoleOf(message) == ai.RoleToolResult {
			t.Fatalf("request carried an orphaned tool result:\n%s", dumpRequestRoles(requests[0]))
		}
		if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopLength {
			t.Fatalf("request replayed the truncated response:\n%s", dumpRequestRoles(requests[0]))
		}
	}
}

// dumpRequestRoles renders the request roles for a failure message.
func dumpRequestRoles(messages []ai.Message) string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		role := ai.RoleOf(message)
		if result, ok := message.(*ai.ToolResultMessage); ok {
			role += "(" + result.ToolCallID + ")"
		}
		roles = append(roles, role)
	}
	return strings.Join(roles, ", ")
}
