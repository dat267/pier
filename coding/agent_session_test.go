package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// AgentSession tests keyed to upstream agent-session.ts semantics.

func mockSessionStreamFn(responses ...*ai.AssistantMessage) agent.StreamFn {
	var mu sync.Mutex
	return func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			mu.Lock()
			var response *ai.AssistantMessage
			if len(responses) > 0 {
				response = responses[0]
				responses = responses[1:]
			}
			mu.Unlock()
			if response == nil {
				response = createAssistantMessageT("(exhausted)")
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: response.StopReason, Message: response})
		}()
		return stream
	}
}

func TestAgentSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000},
		StreamFn: mockSessionStreamFn(createAssistantMessageT("reply")),
		Sessions: sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.PromptText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	entries := sessions.GetEntries()
	// The prompt/tool loadout system message, then user + assistant persisted
	// from agent events.
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	role := func(raw json.RawMessage) string {
		var probe struct {
			Role string `json:"role"`
		}
		json.Unmarshal(raw, &probe)
		return probe.Role
	}
	if role(entries[0].Message) != "system" || role(entries[1].Message) != "user" || role(entries[2].Message) != "assistant" {
		t.Fatalf("roles = %s/%s/%s", role(entries[0].Message), role(entries[1].Message), role(entries[2].Message))
	}
}

func TestAgentSessionQueueTracking(t *testing.T) {
	dir := t.TempDir()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000},
		StreamFn: mockSessionStreamFn(createAssistantMessageT("r1"), createAssistantMessageT("r2")),
		Sessions: NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir}),
	})
	if err != nil {
		t.Fatal(err)
	}

	var queueUpdates []string
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionQueueUpdate {
			queueUpdates = append(queueUpdates, strings.Join(event.Steering, "|"))
		}
	})

	session.Steer(createUserMessage("steered"))
	if len(queueUpdates) != 1 {
		t.Fatalf("queue updates = %v", queueUpdates)
	}
	// Run: the steering message is delivered and removed from the display
	// queue at message_start.
	if err := session.PromptText(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if len(session.steeringMessages) != 0 {
		t.Fatalf("steering display queue = %v", session.steeringMessages)
	}
}

func TestAgentSessionStats(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	session, _ := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000},
		StreamFn: mockSessionStreamFn(&ai.AssistantMessage{
			Content: ai.ContentList{
				ai.TextContent{Text: "reply"},
				ai.ToolCall{ID: "t1", Name: "bash", Arguments: json.RawMessage(`{}`)},
			},
			API: "openai-responses", Provider: "openai", Model: "mock",
			Usage:      ai.Usage{Input: 100, Output: 50, TotalTokens: 150, Cost: ai.UsageCost{Total: 0.5}},
			StopReason: ai.StopToolUse, Timestamp: 1,
		}, &ai.AssistantMessage{
			Content: ai.ContentList{ai.TextContent{Text: "done"}},
			API:     "openai-responses", Provider: "openai", Model: "mock",
			Usage:      ai.Usage{Input: 30, Output: 10, TotalTokens: 40, Cost: ai.UsageCost{Total: 0.1}},
			StopReason: ai.StopStop, Timestamp: 2,
		}),
		Sessions: sessions,
	})
	// Tool call round: bash result.
	session.Agent.SetTools([]agent.AgentTool{makeTool("bash")})
	if err := session.PromptText(context.Background(), "run"); err != nil {
		t.Fatal(err)
	}

	stats := session.GetSessionStats()
	// One prompt: user + assistant(toolUse) + toolResult + assistant(stop),
	// plus the persisted tool-declaration system message.
	if stats.UserMessages != 1 || stats.AssistantMessages != 2 || stats.ToolCalls != 1 || stats.ToolResults != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.TotalMessages != 5 {
		t.Fatalf("totalMessages = %d; want 5 (incl. tool-declaration system message)", stats.TotalMessages)
	}
	// Usage aggregated across the whole run.
	if stats.Tokens.Input != 130 || stats.Tokens.Output != 60 || stats.Tokens.Total != 190 {
		t.Fatalf("tokens = %+v", stats.Tokens)
	}
	if stats.Cost != 0.6 {
		t.Fatalf("cost = %v", stats.Cost)
	}
	if stats.ContextUsage == nil || stats.ContextUsage.ContextWindow != 200000 {
		t.Fatalf("contextUsage = %+v", stats.ContextUsage)
	}
}

func TestAgentSessionAutoRetryDecision(t *testing.T) {
	dir := t.TempDir()
	session, _ := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000},
		StreamFn: mockSessionStreamFn(),
		Sessions: NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir}),
		Settings: SessionSettings{Retry: &ai.RetryPolicy{Enabled: true, MaxRetries: 2, BaseDelayMS: 10}},
	})

	// Transient error → willRetry.
	msg := "terminated"
	errored := &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: &msg}
	event := &agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{errored}}
	if !session.willRetryAfterAgentEnd(event) {
		t.Fatal("transient error should retry")
	}

	// Quota/billing exhaustion never retries.
	quota := "insufficient_quota"
	errored2 := &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: &quota}
	if session.willRetryAfterAgentEnd(&agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{errored2}}) {
		t.Fatal("quota errors must not retry")
	}

	// Retry budget exhausted.
	session.retryAttempt = 2
	if session.willRetryAfterAgentEnd(&agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{errored}}) {
		t.Fatal("exhausted retries must stop")
	}
}

func TestParseSkillBlockExtraction(t *testing.T) {
	text := "<skill name=\"debugging\" location=\"/x/SKILL.md\">\nSkill instructions.\n</skill>\n\nNow debug it."
	parsed := ParseSkillBlock(text)
	if parsed == nil {
		t.Fatal("expected skill block")
	}
	if parsed.Name != "debugging" || parsed.Location != "/x/SKILL.md" || parsed.Content != "Skill instructions." || parsed.UserMessage != "Now debug it." {
		t.Fatalf("parsed = %+v", parsed)
	}
	if ParseSkillBlock("no block here") != nil {
		t.Fatal("non-skill text should not parse")
	}
}

func TestThinkingLevelDefaults(t *testing.T) {
	if DefaultThinkingLevel != ai.ThinkMedium {
		t.Fatal("default thinking level")
	}
	if len(ThinkingLevelOptions) != 7 || ThinkingLevelOptions[0] != ai.ThinkOff {
		t.Fatal("thinking level options")
	}
}

func makeTool(name string) agent.AgentTool {
	return agent.AgentTool{
		Name: name, Description: name + " tool", Parameters: json.RawMessage(`{"type":"object"}`),
		Label: name,
		Execute: func(context.Context, string, json.RawMessage, func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			return agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: name}}, Details: json.RawMessage(`{}`)}, nil
		},
	}
}

// Upstream agent-session.ts (pi v1.5.0 f9a26365) abort() aborts retry,
// compaction, branch summary and the agent run, not a user bash command; only
// dispose (and the dedicated abortBash) stop that. So Escape during a turn and
// /compact leave a running `!command` alone, while AbortAsync, used for
// teardown, still stops it.
func TestAbortLeavesAUserBashCommandRunning(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Model: model, StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = session.ExecuteBash(context.Background(), "sleep 30", nil, nil)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !session.IsBashRunning() {
		if time.Now().After(deadline) {
			t.Fatal("the bash command never started")
		}
		time.Sleep(time.Millisecond)
	}

	session.Abort(context.Background())
	session.AbortTurnAsync()
	// The kill is asynchronous: give a wrongly stopped command time to end.
	select {
	case <-done:
		t.Fatal("aborting the turn stopped the user's bash command")
	case <-time.After(300 * time.Millisecond):
	}

	session.AbortAsync()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AbortAsync did not stop the bash command")
	}
}

// Bash results recorded at once on an idle session (SDK and RPC callers can
// run several commands) all reach the transcript: each append is one step on
// the agent's messages, not a read followed by a later replace that drops
// whatever another append added in between. Upstream recordBashResult
// (agent-session.ts, v1.5.0 f9a26365) appends synchronously on one event loop.
func TestConcurrentBashResultsAreAllRecorded(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := newPromptSession(t, model, unusedSessionStreamFn)
	manager, _ := newTestSession(t)
	session.Sessions = manager
	const commands = 32
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := range commands {
		group.Go(func() {
			<-start
			session.RecordBashResult("echo "+strconv.Itoa(index), BashResult{Output: "ok", ExitCode: new(0)}, false)
		})
	}
	close(start)
	group.Wait()
	got := 0
	for _, message := range session.Agent.State().Messages {
		if ai.RoleOf(message) == RoleBashExecution {
			got++
		}
	}
	if got != commands {
		t.Fatalf("transcript has %d of %d bash results", got, commands)
	}
}

// TestNewAgentSessionHasCollaborators pins the constructor contract: a session
// always carries its collaborator block, so no method has to ask whether the
// session was half-built, and the accessors keep their documented empty answers
// for a session built without a runtime, settings, or tools.
func TestNewAgentSessionHasCollaborators(t *testing.T) {
	manager, _ := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	if session.control == nil {
		t.Fatal("NewAgentSession returned a session without its collaborator block")
	}
	if runtime := session.ModelRuntime(); runtime != nil {
		t.Fatalf("model runtime = %v; want nil without one", runtime)
	}
	if models := session.ScopedModels(); models != nil {
		t.Fatalf("scoped models = %#v; want nil", models)
	}
	if templates := session.PromptTemplates(); templates != nil {
		t.Fatalf("prompt templates = %#v; want nil", templates)
	}
	if tools := session.GetAllTools(); tools != nil {
		t.Fatalf("tools = %#v; want nil", tools)
	}
	if tool := session.GetToolDefinition("read"); tool != nil {
		t.Fatalf("tool = %v; want nil", tool)
	}
	// These are no-ops on an empty registry instead of returning early.
	session.SetActiveToolsByName([]string{"read"})
	session.SetScopedModels([]ScopedModel{{Model: &ai.Model{Provider: "anthropic", ID: "claude-opus-4-5"}}})
	if models := session.ScopedModels(); len(models) != 1 {
		t.Fatalf("scoped models = %#v", models)
	}
	session.SetScopedModels(nil)
	if models := session.ScopedModels(); models != nil {
		t.Fatalf("scoped models = %#v after clearing; want nil", models)
	}
	session.SetAutoRetryEnabled(true)
	if !session.AutoRetryEnabled() {
		t.Fatal("the auto-retry toggle is not stored")
	}
	session.SetAutoCompactionEnabled(true)
	if !session.AutoCompactionEnabled() {
		t.Fatal("the auto-compaction toggle is not stored")
	}
}

// An image-only steer carries no text, so its display entry is "". The
// reference guards the dequeue with `if (messageText)` (agent-session.ts:1141,
// pi v1.5.0 f9a26365), so an empty entry is never removed and the pending
// count never returns to zero. pier removes it.
//
// D437: remove the empty entry, so the dock banner clears when the message is
// pushed into the transcript.
func TestQueueDisplayRemovalHandlesEmptyText(t *testing.T) {
	session := &AgentSession{steeringMessages: []string{""}}
	session.handleAgentEvent(&agent.AgentEvent{
		Type: agent.MessageStart,
		Message: &ai.UserMessage{
			Content: ai.StringOrBlocks{Blocks: ai.ContentList{
				ai.ImageContent{Data: "abc", MimeType: "image/png"},
			}},
		},
	})
	if len(session.steeringMessages) != 0 {
		t.Fatalf("steering display queue = %q, want empty", session.steeringMessages)
	}
}

// Reference: pi v1.5.0 f9a26365 core/agent-session.ts waitForIdle registers
// its promise synchronously in the single-threaded reference. In Go a run can
// finish between the idle check and waiter registration; registering first and
// rechecking keeps that completion from being lost.
func TestIdleWaitRegistrationAfterCompletionDoesNotBlock(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000}
	session := newPromptSession(t, model, unreachableStreamFn)

	// A completion that already happened before registration must report idle
	// instead of returning a channel no completion will ever close.
	if wait := session.registerIdleWait(); wait != nil {
		t.Fatal("idle registration returned a wait channel")
	}

	// While a run is active the same registration returns the channel that
	// completion resolves.
	state := session.prompt()
	state.mu.Lock()
	state.runActive = true
	state.mu.Unlock()
	wait := session.registerIdleWait()
	if wait == nil {
		t.Fatal("active registration reported idle")
	}
	state.mu.Lock()
	state.runActive = false
	state.mu.Unlock()
	session.resolveIdleWaitIfIdle()
	select {
	case <-wait:
	default:
		t.Fatal("completion did not resolve the registered waiter")
	}
}

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts getEntryCount()
// uses the existing ID index rather than copying the entry array. D237's
// initial-tool declaration only needs to know whether a prior entry exists.
func TestAgentSessionInitializationDoesNotCopyHistory(t *testing.T) {
	const count = 512
	version := CurrentSessionVersion
	cwd := t.TempDir()
	entries := make([]FileEntry, count+1)
	entries[0].Header = &SessionHeader{Version: &version, ID: "session", Cwd: cwd}
	for i := range count {
		entries[i+1].Entry = &SessionEntry{Type: "custom", ID: fmt.Sprint(i), CustomType: "test"}
	}
	manager := InMemorySession(cwd, nil, entries)
	config := &SessionConfig{Cwd: cwd, Sessions: manager, StreamFn: mockSessionStreamFn()}
	warm, err := NewAgentSession(config)
	if err != nil {
		t.Fatal(err)
	}
	warm.Dispose()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	session, err := NewAgentSession(config)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	// Allow ordinary agent/control allocation, but not a 225 KiB snapshot
	// of history just to test whether it is empty.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64*1024 {
		t.Fatalf("initializing over %d entries allocated %d bytes, want at most 65536", count, allocated)
	}
	if len(manager.GetEntries()) != count {
		t.Fatal("initialization appended an initial declaration to a nonempty session")
	}
}

// pi v1.4.0 coding-agent/src/utils/sleep.ts uses setTimeout(ms), whose runtime
// clamps values above 2^31-1 to 1ms instead of sleeping for days.
func TestPrepareRetryDelayOverflowMatchesJavaScriptTimeout(t *testing.T) {
	policy := &ai.RetryPolicy{
		Enabled: true, MaxRetries: 1, BaseDelayMS: 1 << 31, MaxAgentDelayMS: 1 << 31,
	}
	session := retrySession(t, policy, erroringStreamFn(0, ""))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	message := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		StopReason: ai.StopError, ErrorMessage: new("overloaded_error"),
	}
	retry, err := session.PrepareRetry(ctx, message)
	if err != nil {
		t.Fatalf("prepare retry: %v", err)
	}
	if !retry {
		t.Fatal("retry did not run after the overflowing delay")
	}
}
