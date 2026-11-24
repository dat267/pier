package coding

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// A displaced call wakes the model exactly once, with one notice carrying the
// result. The session is idle when the call ends, so the notice starts a run
// itself: queued alone it would wait for the user's next message. It is an
// ordinary user message, so the transcript shape is the reference's.
func TestSessionWakesOncePerBackgroundCall(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	handle := agent.ToolCallHandle{
		ID: "bg-1", ToolCallID: "c1", ToolName: "bash", OutputPath: "/tmp/out.log",
		Wait: func() agent.AgentToolResult {
			return agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "the slow output"}}}
		},
	}
	session.watchBackgroundCall(handle)

	notices := func() []string {
		var found []string
		for _, message := range session.Agent.State().Messages {
			if text := backgroundNoticeText(message); strings.Contains(text, "bg-1") {
				found = append(found, text)
			}
		}
		return found
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(notices()) == 0 {
		time.Sleep(time.Millisecond)
	}
	if err := session.WaitForIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	found := notices()
	if len(found) != 1 {
		t.Fatalf("an idle session ran %d notices, want exactly one", len(found))
	}
	for _, want := range []string{"bg-1", "bash", "the slow output"} {
		if !strings.Contains(found[0], want) {
			t.Errorf("the notice lacks %q: %q", want, found[0])
		}
	}
	// The run answered it, and nothing is left queued for a second wake-up.
	if last, ok := session.Agent.State().Messages[len(session.Agent.State().Messages)-1].(*ai.AssistantMessage); !ok || last.StopReason != ai.StopStop {
		t.Fatalf("the notice's run did not end with the model's reply")
	}
	time.Sleep(20 * time.Millisecond)
	if extra := session.Agent.TakeFollowUpMessages(); len(extra) != 0 || len(notices()) != 1 {
		t.Fatalf("a second wake-up arrived: %d queued, %d in the transcript", len(extra), len(notices()))
	}
}

// A notice that arrives while a run is active is queued as a follow-up, so that
// run's stop boundary delivers it; it does not start a second run.
func TestBackgroundNoticeQueuesDuringARun(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	before := len(session.Agent.State().Messages)
	state := session.prompt()
	state.mu.Lock()
	state.runActive = true
	state.mu.Unlock()
	session.deliverBackgroundNotice(agent.CompletionNotice(
		agent.ToolCallHandle{ID: "bg-3", ToolCallID: "c3", ToolName: "bash"},
		agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}}))
	state.mu.Lock()
	state.runActive = false
	state.mu.Unlock()

	queued := session.Agent.TakeFollowUpMessages()
	if len(queued) != 1 || !strings.Contains(backgroundNoticeText(queued[0]), "bg-3") {
		t.Fatalf("queued = %d, want the one notice", len(queued))
	}
	if after := len(session.Agent.State().Messages); after != before {
		t.Fatalf("a notice during a run started another: transcript %d -> %d messages", before, after)
	}
}

// backgroundNoticeText is a user message's text, or "" for any other message.
func backgroundNoticeText(message ai.Message) string {
	user, ok := message.(*ai.UserMessage)
	if !ok {
		return ""
	}
	text := user.Content.Text
	for _, block := range user.Content.Blocks {
		if value, ok := block.(ai.TextContent); ok {
			text += value.Text
		}
	}
	return text
}

// A disposed session does not queue a notice: there is nobody to wake.
func TestSessionSkipsWakeAfterDispose(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read")},
	})
	if err != nil {
		t.Fatal(err)
	}
	agentRef := session.Agent
	handle := agent.ToolCallHandle{
		ID: "bg-2", ToolCallID: "c2", ToolName: "bash",
		Wait: func() agent.AgentToolResult {
			return agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "late"}}}
		},
	}
	session.Dispose()
	session.watchBackgroundCall(handle)
	time.Sleep(50 * time.Millisecond)
	if notices := agentRef.TakeFollowUpMessages(); len(notices) != 0 {
		t.Fatalf("a disposed session queued %d notices", len(notices))
	}
}

// A real turn with a real shell: the model calls bash, the command streams a lot
// and outlives the threshold, and the turn returns while the shell keeps running.
// The output file must be readable mid-run, because that is what the handle is
// for, and must hold the finished output once the shell exits.
func TestBackgroundRealShellCallStreamsToFile(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a shell that sleeps past the threshold")
	}
	manager, dir := newTestSession(t)
	bashTool := CreateBashTool(dir, nil)
	script := []*ai.AssistantMessage{
		{API: "anthropic-messages", Provider: "anthropic", Model: "m", StopReason: ai.StopToolUse,
			Content: ai.ContentList{ai.ToolCall{
				ID: "call-bg", Name: "bash",
				Arguments: []byte(`{"command":"seq 1 300; sleep 5; echo FINISHED-MARKER"}`),
			}}},
		{API: "anthropic-messages", Provider: "anthropic", Model: "m", StopReason: ai.StopStop,
			Content: ai.ContentList{ai.TextContent{Text: "I have a handle."}}},
	}
	next := 0
	streamFn := func(_ *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		message := script[next]
		if next < len(script)-1 {
			next++
		}
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: streamFn, ConvertToLlm: ConvertToLlm,
		Model:                &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools:                []agent.AgentTool{bashTool},
		BackgroundToolCalls:  func() time.Duration { return 2 * time.Second },
		BackgroundOutputPath: BackgroundToolOutputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	if err := session.PromptText(t.Context(), "run it"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	raw, err := os.ReadFile(manager.GetSessionFile())
	if err != nil {
		t.Fatalf("session file: %v", err)
	}
	if !strings.Contains(string(raw), "still running") {
		t.Fatalf("the turn did not return a handle:\n%s", raw)
	}
	marker := `"outputPath":"`
	at := strings.Index(string(raw), marker)
	if at < 0 {
		t.Fatalf("the handle carries no outputPath detail:\n%s", raw)
	}
	rest := string(raw)[at+len(marker):]
	path := rest[:strings.Index(rest, `"`)]
	defer os.Remove(path)

	// Readable while the shell is still running: this is what the agent reads.
	deadline := time.Now().Add(10 * time.Second)
	var early []byte
	for time.Now().Before(deadline) {
		early, _ = os.ReadFile(path)
		if len(early) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(early) == 0 {
		t.Fatal("the output file was empty while the shell ran, so the handle points at nothing")
	}
	if strings.Contains(string(early), "FINISHED-MARKER") {
		t.Fatalf("the file already held the finished output mid-run:\n%s", early)
	}

	// And it holds the finished output once the shell exits.
	for time.Now().Before(deadline.Add(10 * time.Second)) {
		if final, err := os.ReadFile(path); err == nil && strings.Contains(string(final), "FINISHED-MARKER") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	final, _ := os.ReadFile(path)
	t.Fatalf("the finished output never reached the file:\n%s", final)
}
