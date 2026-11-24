package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// newBackgroundTestTool builds a tool whose Execute blocks on release and counts
// its calls, so a test can hold a call open and then let it finish.
func newBackgroundTestTool(name string, ops *backgroundTestOps) AgentTool {
	return AgentTool{
		Name: name,
		Execute: func(ctx context.Context, _ string, _ json.RawMessage, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			atomic.AddInt32(&ops.started, 1)
			onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "working"}}})
			if ops.release != nil {
				select {
				case <-ops.release:
				case <-ctx.Done():
					return AgentToolResult{}, ctx.Err()
				}
			}
			if ops.failWith != nil {
				return AgentToolResult{}, ops.failWith
			}
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: ops.output}}}, nil
		},
	}
}

// backgroundTestOps holds a call open and records how many ran.
type backgroundTestOps struct {
	started  int32
	release  chan struct{}
	output   string
	failWith error
}

// A call that finishes inside the threshold takes the ordinary path: the same
// result, the same events, and no handle.
func TestBackgroundFastCallIsUnchanged(t *testing.T) {
	prepared := func(run *backgroundTestOps) *preparedToolCallValue {
		tool := newBackgroundTestTool("bash", run)
		return &preparedToolCallValue{
			toolCall: ai.ToolCall{ID: "c1", Name: "bash"},
			tool:     &tool,
		}
	}
	var events []string
	sink := func(event AgentEvent) error {
		events = append(events, event.Type)
		return nil
	}

	// Without a policy.
	plain := &backgroundTestOps{release: closedChannel(), output: "done"}
	gotPlain := executePreparedToolCall(context.Background(), prepared(plain), sink, nil)

	// With a threshold the call beats.
	var registered int
	withPolicy := &backgroundTestOps{release: closedChannel(), output: "done"}
	registry := newBackgroundRegistry()
	policy := &backgroundPolicy{
		threshold:  time.Minute,
		registry:   registry,
		outputPath: func(string) string { return "/tmp/out" },
		onRegister: func(ToolCallHandle) { registered++ },
		nextID:     func() string { return "bg-1" },
	}
	gotPolicy := executePreparedToolCall(context.Background(), prepared(withPolicy), sink, policy)

	if registered != 0 {
		t.Fatalf("a fast call was backgrounded (%d registrations)", registered)
	}
	if gotPlain.isError != gotPolicy.isError {
		t.Fatalf("isError differs: %v vs %v", gotPlain.isError, gotPolicy.isError)
	}
	if text(gotPlain.result) != text(gotPolicy.result) {
		t.Fatalf("result text differs:\n plain %q\n policy %q", text(gotPlain.result), text(gotPolicy.result))
	}
	if gotPlain.result.Details != nil || gotPolicy.result.Details != nil {
		t.Fatalf("details differ from the plain path: %v / %v", gotPlain.result.Details, gotPolicy.result.Details)
	}
	if registry.active() != 0 {
		t.Fatalf("registry holds %d calls after a fast call", registry.active())
	}
}

// A call that outlives the threshold returns a handle instead of a result, keeps
// running, and reports the handle to the host.
func TestBackgroundSlowCallReturnsHandle(t *testing.T) {
	run := &backgroundTestOps{release: make(chan struct{}), output: "collected output"}
	var handles []ToolCallHandle
	registry := newBackgroundRegistry()
	policy := &backgroundPolicy{
		threshold:  10 * time.Millisecond,
		registry:   registry,
		outputPath: func(string) string { return "/tmp/background-out.log" },
		onRegister: func(handle ToolCallHandle) { handles = append(handles, handle) },
		nextID:     func() string { return "bg-7" },
	}

	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "c2", Name: "bash"},
		tool:     pointerTo(newBackgroundTestTool("bash", run)),
	}
	start := time.Now()
	outcome, backgrounded := executeWithBackground(context.Background(), prepared, func(AgentEvent) error { return nil }, policy)
	if !backgrounded {
		t.Fatal("the call was not backgrounded")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("returning the handle took %v", elapsed)
	}
	if outcome.isError {
		t.Fatal("a handle was reported as an error result")
	}
	body := text(outcome.result)
	for _, want := range []string{"bg-7", "/tmp/background-out.log", "still running", "do not call it again"} {
		if !strings.Contains(body, want) {
			t.Errorf("the handle text lacks %q:\n%s", want, body)
		}
	}
	if len(handles) != 1 || handles[0].ID != "bg-7" {
		t.Fatalf("handles = %+v, want one for bg-7", handles)
	}

	// The call is still running: releasing it must produce the real result.
	close(run.release)
	waited := make(chan AgentToolResult, 1)
	go func() { waited <- handles[0].Wait() }()
	var final AgentToolResult
	select {
	case final = <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("the backgrounded call never reported")
	}
	if got := text(final); !strings.Contains(got, "collected output") {
		t.Fatalf("the final result = %q", got)
	}
	if registry.active() != 0 {
		t.Fatalf("registry still holds %d calls after the call ended", registry.active())
	}
}

// A backgrounded call that fails still reports exactly once, as a failure.
func TestBackgroundFailedCallReportsOnce(t *testing.T) {
	run := &backgroundTestOps{release: make(chan struct{}), failWith: errors.New("boom")}
	var handles []ToolCallHandle
	policy := &backgroundPolicy{
		threshold:  time.Millisecond,
		registry:   newBackgroundRegistry(),
		outputPath: func(string) string { return "/tmp/out" },
		onRegister: func(handle ToolCallHandle) { handles = append(handles, handle) },
		nextID:     func() string { return "bg-9" },
	}
	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "c3", Name: "bash"},
		tool:     pointerTo(newBackgroundTestTool("bash", run)),
	}
	if _, backgrounded := executeWithBackground(context.Background(), prepared, func(AgentEvent) error { return nil }, policy); !backgrounded {
		t.Fatal("the call was not backgrounded")
	}
	close(run.release)

	waited := make(chan AgentToolResult, 1)
	go func() { waited <- handles[0].Wait() }()
	var result AgentToolResult
	select {
	case result = <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("the failed call never reported")
	}
	if !result.IsError {
		t.Fatal("a failed backgrounded call was not reported as an error")
	}
	notice := CompletionNotice(handles[0], result)
	if got := messageText(notice); !strings.Contains(got, "failed") {
		t.Fatalf("the notice does not say it failed: %q", got)
	}
}

// Closing the registry cancels the displaced calls, so a settled run leaves no
// process behind and no goroutine waiting on one.
func TestBackgroundRegistryCloseCancels(t *testing.T) {
	run := &backgroundTestOps{release: make(chan struct{}), output: "never"}
	registry := newBackgroundRegistry()
	policy := &backgroundPolicy{
		threshold:  time.Millisecond,
		registry:   registry,
		outputPath: func(string) string { return "/tmp/out" },
		onRegister: func(ToolCallHandle) {},
		nextID:     func() string { return "bg-x" },
	}
	var cancelled sync.WaitGroup
	cancelled.Add(1)
	tool := AgentTool{
		Name: "bash",
		Execute: func(ctx context.Context, _ string, _ json.RawMessage, _ func(AgentToolResult)) (AgentToolResult, error) {
			defer cancelled.Done()
			<-ctx.Done()
			return AgentToolResult{}, ctx.Err()
		},
	}
	prepared := &preparedToolCallValue{toolCall: ai.ToolCall{ID: "c4", Name: "bash"}, tool: &tool}
	if _, backgrounded := executeWithBackground(context.Background(), prepared, func(AgentEvent) error { return nil }, policy); !backgrounded {
		t.Fatal("the call was not backgrounded")
	}
	if registry.active() != 1 {
		t.Fatalf("registry holds %d calls, want 1", registry.active())
	}
	registry.close()
	if registry.active() != 0 {
		t.Fatalf("registry holds %d calls after close", registry.active())
	}
	waitWithTimeout(t, &cancelled, 5*time.Second, "the displaced call was not cancelled")
	// A registration after close is refused rather than silently dropped.
	if registry.register(&backgroundCall{cancel: func() {}}) {
		t.Fatal("a closed registry accepted a new call")
	}
	_ = run
}

// A nil policy never constructs a registry, so a run without a threshold pays
// nothing and behaves exactly as the reference does.
func TestBackgroundDisabledByDefault(t *testing.T) {
	var agent Agent
	if agent.BackgroundToolCalls != nil {
		t.Fatal("backgrounding is enabled by default")
	}
	config := &AgentLoopConfig{}
	if config.backgroundPolicy != nil {
		t.Fatal("an empty loop config carries a background policy")
	}
}

// closedChannel is a channel that is already closed, so a call returns at once.
func closedChannel() chan struct{} {
	channel := make(chan struct{})
	close(channel)
	return channel
}

// pointerTo returns a pointer to its argument.
func pointerTo[T any](value T) *T { return &value }

// waitWithTimeout waits for a WaitGroup or fails.
func waitWithTimeout(t *testing.T, group *sync.WaitGroup, limit time.Duration, message string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatal(message)
	}
}

// text joins a tool result's text blocks.
func text(result AgentToolResult) string {
	var body strings.Builder
	for _, block := range result.Content {
		if value, ok := block.(ai.TextContent); ok {
			body.WriteString(value.Text)
		}
	}
	return body.String()
}

// messageText joins a message's text content.
func messageText(message ai.Message) string {
	user, ok := message.(*ai.UserMessage)
	if !ok {
		return ""
	}
	if user.Content.Blocks == nil {
		return user.Content.Text
	}
	var body strings.Builder
	for _, block := range user.Content.Blocks {
		if value, ok := block.(ai.TextContent); ok {
			body.WriteString(value.Text)
		}
	}
	return body.String()
}

// A real agent run whose tool outlives the threshold returns the handle to the
// model in that turn's tool result, keeps the process running, and reports the
// handle to the host once.
func TestBackgroundThroughARun(t *testing.T) {
	release := make(chan struct{})
	var running int32
	slow := AgentTool{
		Name:        "slow",
		Description: "a slow call",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, _ string, _ json.RawMessage, _ func(AgentToolResult)) (AgentToolResult, error) {
			atomic.AddInt32(&running, 1)
			select {
			case <-release:
			case <-ctx.Done():
				return AgentToolResult{}, ctx.Err()
			}
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "finished late"}}}, nil
		},
	}
	handles := make(chan ToolCallHandle, 1)
	streamFn, _ := mockStreamFn(
		createAssistantMessage(ai.ContentList{
			ai.ToolCall{ID: "call-1", Name: "slow", Arguments: json.RawMessage(`{}`)},
		}, ai.StopToolUse),
	)
	agentInstance, err := NewAgent(&AgentOptions{
		InitialState: &AgentInitialState{
			Model: createModel(),
			Tools: []AgentTool{slow},
		},
		StreamFn:            streamFn,
		ToolExecution:       ToolExecutionSequential,
		BackgroundToolCalls: func() time.Duration { return 10 * time.Millisecond },
		BackgroundOutputPath: func(string) string {
			return "/tmp/agent-background.log"
		},
		OnToolBackgrounded: func(handle ToolCallHandle) { handles <- handle },
	})
	if err != nil {
		t.Fatal(err)
	}

	sink := &recordingSink{}
	unsubscribe := agentInstance.Subscribe(sink.listener)
	defer unsubscribe()
	if err := agentInstance.PromptMessages(context.Background(), []ai.Message{createUserMessage("go")}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var handle ToolCallHandle
	select {
	case handle = <-handles:
	case <-time.After(5 * time.Second):
		t.Fatal("the host was never told about the backgrounded call")
	}
	if handle.ID == "" || handle.OutputPath != "/tmp/agent-background.log" {
		t.Fatalf("handle = %+v", handle)
	}
	if atomic.LoadInt32(&running) != 1 {
		t.Fatalf("the tool ran %d times, want 1", running)
	}
	var sawHandle bool
	for _, message := range sink.messages() {
		if strings.Contains(message, "still running") {
			sawHandle = true
		}
	}
	if !sawHandle {
		t.Fatalf("the transcript holds no handle:\n%s", strings.Join(sink.messages(), "\n---\n"))
	}

	close(release)
	waited := make(chan AgentToolResult, 1)
	go func() { waited <- handle.Wait() }()
	select {
	case result := <-waited:
		if !strings.Contains(text(result), "finished late") {
			t.Fatalf("final result = %q", text(result))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the backgrounded call never finished")
	}
}

// recordingSink collects the tool-result text a run produced.
type recordingSink struct {
	mu   sync.Mutex
	seen []string
}

func (r *recordingSink) listener(ctx context.Context, event AgentEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch event.Type {
	case ToolExecutionEnd:
		for _, block := range event.Result.Content {
			if value, ok := block.(ai.TextContent); ok {
				r.seen = append(r.seen, value.Text)
			}
		}
	}
	return nil
}

func (r *recordingSink) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.seen...)
}

// CloseBackgroundCalls cancels the displaced calls and refuses new ones, which is
// what the session calls on dispose: a closed pier leaves no orphan process.
func TestCloseBackgroundCallsCancels(t *testing.T) {
	cancelled := make(chan struct{})
	agentInstance, err := NewAgent(&AgentOptions{
		InitialState: &AgentInitialState{Model: createModel()},
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return ai.NewAssistantMessageEventStream()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := agentInstance.backgroundRegistry()
	call := &backgroundCall{
		cancel:   func() { close(cancelled) },
		finished: make(chan struct{}),
	}
	if !registry.register(call) {
		t.Fatal("the registry refused a call")
	}
	if registry.active() != 1 {
		t.Fatalf("registry holds %d calls, want 1", registry.active())
	}
	agentInstance.CloseBackgroundCalls()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("closing did not cancel the displaced call")
	}
	if fresh := agentInstance.backgroundRegistry(); fresh.active() != 0 {
		t.Fatalf("the fresh registry holds %d calls", fresh.active())
	}
}

// A displaced call's output accumulates in the file the handle names, and the
// file exists from the moment the handle is returned.
func TestBackgroundWritesOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background.log")
	release := make(chan struct{})
	tool := AgentTool{
		Name:        "slow",
		Description: "writes then waits",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, _ string, _ json.RawMessage, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "partial line"}}})
			select {
			case <-release:
			case <-ctx.Done():
				return AgentToolResult{}, ctx.Err()
			}
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "final line"}}}, nil
		},
	}
	var handle ToolCallHandle
	policy := &backgroundPolicy{
		threshold:  10 * time.Millisecond,
		registry:   newBackgroundRegistry(),
		outputPath: func(string) string { return path },
		onRegister: func(got ToolCallHandle) { handle = got },
		nextID:     func() string { return "bg-file" },
	}
	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "cfile", Name: "slow"},
		tool:     &tool,
	}
	if _, backgrounded := executeWithBackground(context.Background(), prepared, func(AgentEvent) error { return nil }, policy); !backgrounded {
		t.Fatal("the call was not backgrounded")
	}
	if handle.OutputPath != path {
		t.Fatalf("handle path = %q, want %q", handle.OutputPath, path)
	}
	// The file exists as soon as the handle is returned.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the output file does not exist: %v", err)
	}

	close(release)
	waited := make(chan AgentToolResult, 1)
	go func() { waited <- handle.Wait() }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never finished")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(raw), "final line") {
		t.Fatalf("the output file lacks the final result:\n%s", raw)
	}
}
