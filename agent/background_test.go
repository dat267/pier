package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

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
	// The tool must have run exactly once, with and without the policy. Equal
	// results hid a double execution, which is invisible for an idempotent tool
	// and destructive for one that is not: cron_create ran twice and armed two
	// jobs, and cron_delete's second run reported "No scheduled job" for the job
	// its first run had already removed.
	if got := atomic.LoadInt32(&plain.started); got != 1 {
		t.Fatalf("without a policy the tool ran %d times", got)
	}
	if got := atomic.LoadInt32(&withPolicy.started); got != 1 {
		t.Fatalf("with a policy the tool ran %d times", got)
	}
	// The tool must have run exactly once, with and without the policy. Equal
	// results hid a double execution, which is invisible for an idempotent tool
	// and destructive for one that is not: cron_create ran twice and armed two
	// jobs, and cron_delete's second run reported "No scheduled job" for the job
	// its first run had already removed.
	if got := atomic.LoadInt32(&plain.started); got != 1 {
		t.Fatalf("without a policy the tool ran %d times", got)
	}
	if got := atomic.LoadInt32(&withPolicy.started); got != 1 {
		t.Fatalf("with a policy the tool ran %d times", got)
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
	// The frame shows how long the call ran in the foreground before it moved,
	// not 0.0s: it outlived the threshold, so its duration is at least that.
	if outcome.durationMS < 10 {
		t.Fatalf("the handle's duration = %dms, want at least the 10ms threshold", outcome.durationMS)
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
	// The host can show the call is still running (D538's footer count).
	if got := agentInstance.BackgroundCallCount(); got != 1 {
		t.Fatalf("BackgroundCallCount = %d while the call runs, want 1", got)
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
	deadline := time.Now().Add(5 * time.Second)
	for agentInstance.BackgroundCallCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("BackgroundCallCount = %d after the call ended", agentInstance.BackgroundCallCount())
		}
		runtime.Gosched()
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

// A displaced call's output streams into the file the handle names while it runs,
// not only at the end: a handle whose file is empty until the call finishes tells
// the agent nothing about progress, which is the point of the path.
func TestBackgroundWritesOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background.log")
	release := make(chan struct{})
	streamed := make(chan struct{})
	tool := AgentTool{
		Name:        "slow",
		Description: "streams then waits",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, _ string, _ json.RawMessage, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "partial line"}}})
			close(streamed)
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

	// Readable while the call is still running: the streamed piece is already there.
	<-streamed
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), "partial line") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mid, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the output file mid-run: %v", err)
	}
	if !strings.Contains(string(mid), "partial line") {
		t.Fatalf("the file is not readable while the call runs:\n%q", mid)
	}
	if strings.Contains(string(mid), "final line") {
		t.Fatalf("the file already holds the end while the call runs:\n%q", mid)
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
	// Exactly the final result: the streamed piece was the result so far, so it
	// is replaced rather than left in front of it.
	if string(raw) != "final line\n" {
		t.Fatalf("the output file = %q, want only the final result", raw)
	}
}

// The output file holds the latest snapshot, never a pile of them: a partial
// result is the whole result so far (upstream interactive-mode.ts replaces the
// shown result with each one), so appending repeated every earlier line. A
// failure keeps the output before it unless its text already starts with it.
func TestBackgroundOutputFileHoldsLatestSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pieces  []string
		final   string
		failed  bool
		midWant string
		want    string
	}{
		{name: "cumulative snapshots", pieces: []string{"1", "1\n2", "1\n2\n3\n"}, final: "1\n2\n3\n4",
			midWant: "1\n2\n3\n", want: "1\n2\n3\n4\n"},
		{name: "an empty piece keeps the file", pieces: []string{"progress", ""}, final: "done",
			midWant: "progress\n", want: "done\n"},
		{name: "a shell failure carries its output", pieces: []string{"a\nb"}, failed: true,
			final: "a\nb\n\nCommand exited with code 1", midWant: "a\nb\n", want: "a\nb\n\nCommand exited with code 1\n"},
		{name: "a bare failure keeps the streamed output", pieces: []string{"50% done"}, failed: true,
			final: "connection lost", midWant: "50% done\n", want: "50% done\nconnection lost\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.log")
			output := newBackgroundOutput(path, nil)
			for _, piece := range tc.pieces {
				output.write(piece)
			}
			if mid, _ := os.ReadFile(path); string(mid) != tc.midWant {
				t.Fatalf("mid-run file = %q, want %q", mid, tc.midWant)
			}
			if tc.failed {
				output.writeFailure(tc.final)
			} else {
				output.write(tc.final)
			}
			output.close()
			if raw, _ := os.ReadFile(path); string(raw) != tc.want {
				t.Fatalf("final file = %q, want %q", raw, tc.want)
			}
		})
	}
}

// A call that finishes inside the threshold leaves no scratch file behind: the
// file is opened before the call starts, so a fast call must clean it up.
func TestBackgroundFastCallLeavesNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unused.log")
	policy := &backgroundPolicy{
		threshold:  time.Minute,
		registry:   newBackgroundRegistry(),
		outputPath: func(string) string { return path },
		onRegister: func(ToolCallHandle) {},
		nextID:     func() string { return "bg-none" },
	}
	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "cfast", Name: "quick"},
		tool:     pointerTo(newBackgroundTestTool("quick", &backgroundTestOps{release: closedChannel(), output: "done"})),
	}
	// A fast call is not backgrounded: it reports the outcome it already has, so
	// the caller must not run it again. "not backgrounded" therefore means "handled
	// here", not "finished and run a second time".
	if _, handled := executeWithBackground(context.Background(), prepared, func(AgentEvent) error { return nil }, policy); !handled {
		t.Fatal("a fast call was not handled by the foreground path")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a fast call left %s behind (stat err %v)", path, err)
	}
}

// The wake-up names the output file and caps its own body, so a call that produced
// far more than fits in a message still has all its output available and does not
// flood the transcript it wakes.
// D538: a displaced call's output reaches the model only through this notice, so
// the notice takes pi's model-facing output limit and its cut
// (pi/packages/mcp/src/output-limit.ts: MCP_OUTPUT_MAX_BYTES, truncateMiddle):
// the start and the end survive, the middle becomes a `…N chars truncated…`
// marker, and a warning header states the original size.
func TestBackgroundNoticeNamesFileAndCapsBody(t *testing.T) {
	head := strings.Repeat("H", 30000)
	tail := strings.Repeat("T", 30000)
	handle := ToolCallHandle{ID: "bg-1", ToolCallID: "c1", ToolName: "bash", OutputPath: "/tmp/out.log"}
	notice := CompletionNotice(handle, AgentToolResult{Content: []ai.Content{ai.TextContent{Text: head + tail}}})
	body := messageText(notice)
	if !strings.Contains(body, "/tmp/out.log") {
		t.Fatalf("the notice does not name the output file:\n%.300s", body)
	}
	if !strings.Contains(body, "Warning: truncated output (original token count: ") {
		t.Fatalf("the notice lacks pi's truncation warning:\n%.300s", body)
	}
	if !strings.Contains(body, "Total output lines: 1") {
		t.Fatalf("the notice lacks pi's line count:\n%.300s", body)
	}
	if !regexp.MustCompile(`…\d+ chars truncated…`).MatchString(body) {
		t.Fatalf("the notice lacks pi's middle marker:\n%.300s", body)
	}
	// Both ends survive, which is the point of cutting the middle: a tail-only cut
	// threw away the start of every long call.
	if !strings.Contains(body, "HHHHHHHHHH") || !strings.Contains(body, "TTTTTTTTTT") {
		t.Fatalf("the notice did not keep both ends:\n%.300s", body)
	}
	if len(body) > backgroundNoticeBodyLimit+300 {
		t.Fatalf("the notice is %d bytes, more than the cap of %d", len(body), backgroundNoticeBodyLimit)
	}
	// A short result is carried whole, with no cut marker.
	short := CompletionNotice(handle, AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "all done"}}})
	if got := messageText(short); strings.Contains(got, "truncated") || !strings.Contains(got, "all done") {
		t.Fatalf("a short result was mangled:\n%s", got)
	}
}

// The cut lands on a character boundary: a byte cut through non-ASCII output
// (a test runner's ✓, a localized message) wrote invalid UTF-8 into the
// transcript the notice wakes.
func TestBackgroundNoticeCutKeepsUTF8(t *testing.T) {
	body := strings.Repeat("é", backgroundNoticeBodyLimit) + "x"
	notice := CompletionNotice(ToolCallHandle{ID: "bg-u", ToolName: "bash"},
		AgentToolResult{Content: []ai.Content{ai.TextContent{Text: body}}})
	got := messageText(notice)
	if !utf8.ValidString(got) {
		t.Fatal("the clipped notice is not valid UTF-8")
	}
	if !regexp.MustCompile(`…\d+ chars truncated…`).MatchString(got) || !strings.HasSuffix(got, "éx") {
		t.Fatalf("the notice lost its cut marker or its tail")
	}
}

// D538: the notice is an ordinary user message, so the interactive transcript
// recognizes it by its fixed shape, as upstream recognizes a skill block, and
// draws it as a labeled block instead of a message the user typed.
func TestParseCompletionNotice(t *testing.T) {
	handle := ToolCallHandle{ID: "bg-1a2b3c", ToolName: "bash", OutputPath: "/tmp/pi-background-x.log"}
	notice := CompletionNotice(handle, AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "line one\n**not markdown**"}}})
	title, body, ok := ParseCompletionNotice(messageText(notice))
	if !ok || title != "bg-1a2b3c (bash) finished" {
		t.Fatalf("title = %q, ok = %v", title, ok)
	}
	if !strings.Contains(body, "/tmp/pi-background-x.log") || !strings.HasSuffix(body, "line one\n**not markdown**") {
		t.Fatalf("body = %q", body)
	}
	failed := CompletionNotice(ToolCallHandle{ID: "bg-2", ToolName: "github_search"}, AgentToolResult{IsError: true})
	if title, _, ok := ParseCompletionNotice(messageText(failed)); !ok || title != "bg-2 (github_search) failed" {
		t.Fatalf("failed title = %q, ok = %v", title, ok)
	}
	// A real handle records when the call started, so the notice says how long the
	// call took: a displaced call shows nothing until it finishes, and its duration
	// is what the model needs to judge it.
	timed := handle
	timed.StartedAt = time.Now().Add(-90 * time.Second)
	title, _, ok = ParseCompletionNotice(messageText(CompletionNotice(timed, AgentToolResult{})))
	if !ok || title != "bg-1a2b3c (bash) finished in 1m 30s" {
		t.Fatalf("timed title = %q, ok = %v", title, ok)
	}
	// A failed call reports its duration too.
	timedFail := timed
	timedFail.ID = "bg-9f9f"
	if title, _, ok := ParseCompletionNotice(messageText(CompletionNotice(timedFail, AgentToolResult{IsError: true}))); !ok || title != "bg-9f9f (bash) failed in 1m 30s" {
		t.Fatalf("failed timed title = %q, ok = %v", title, ok)
	}

	// The start notice shares the header, so the transcript draws it the same way.
	started := StartedNotice(handle)
	title, body, ok = ParseCompletionNotice(messageText(started))
	if !ok || title != "bg-1a2b3c (bash) started" {
		t.Fatalf("started title = %q, ok = %v", title, ok)
	}
	if !strings.Contains(body, "/tmp/pi-background-x.log") || !strings.Contains(body, "when it finishes") {
		t.Fatalf("started body = %q", body)
	}
	for _, typed := range []string{"Background call bg-1 (bash) finished.", "please check Background call bg-1 (bash) finished.\n\nok", "hello"} {
		if _, _, ok := ParseCompletionNotice(typed); ok {
			t.Fatalf("%q was taken for a notice", typed)
		}
	}
}

// The host is told the output file is in use for the call's whole life, so its
// scratch sweep cannot delete output the finish notice still names. The claim is
// released once the file closes.
func TestBackgroundOutputClaimedForTheCallsLife(t *testing.T) {
	release := make(chan struct{})
	slow := AgentTool{
		Name:        "slow",
		Description: "a slow call",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, _ string, _ json.RawMessage, _ func(AgentToolResult)) (AgentToolResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
				return AgentToolResult{}, ctx.Err()
			}
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}}, nil
		},
	}
	handles := make(chan ToolCallHandle, 1)
	streamFn, _ := mockStreamFn(
		createAssistantMessage(ai.ContentList{
			ai.ToolCall{ID: "call-1", Name: "slow", Arguments: json.RawMessage(`{}`)},
		}, ai.StopToolUse),
	)
	var claimed, released int32
	agentInstance, err := NewAgent(&AgentOptions{
		InitialState:        &AgentInitialState{Model: createModel(), Tools: []AgentTool{slow}},
		StreamFn:            streamFn,
		ToolExecution:       ToolExecutionSequential,
		BackgroundToolCalls: func() time.Duration { return 10 * time.Millisecond },
		BackgroundOutputPath: func(string) string {
			return filepath.Join(t.TempDir(), "out.log")
		},
		ClaimBackgroundOutput: func(string) func() {
			atomic.AddInt32(&claimed, 1)
			return func() { atomic.AddInt32(&released, 1) }
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
	select {
	case <-handles:
	case <-time.After(5 * time.Second):
		t.Fatal("never backgrounded")
	}
	if atomic.LoadInt32(&claimed) != 1 {
		t.Fatalf("claim calls = %d, want 1", claimed)
	}
	close(release)
	agentInstance.CloseBackgroundCalls()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&released) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&released) != 1 {
		t.Fatalf("release calls = %d, want 1", released)
	}
}
