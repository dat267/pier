package interactive

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// The main loop must never stall more than 100ms (AGENTS.md): any phase over
// that budget is a regression, not an acceptable cost. This drives the
// recurring heavy path — a large streaming assistant message with code fences,
// which historically re-parsed markdown and re-highlighted code on every delta
// — through the real loop and fails if the stall hook fires. Resume and delete
// have their own off-loop tests, since their heavy work must not run on the loop
// at all.
func TestMainLoopStallBudgetStreaming(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()

	var mu sync.Mutex
	var stalls []string
	app.runner.OnStall = func(name string, elapsed time.Duration) {
		mu.Lock()
		stalls = append(stalls, fmt.Sprintf("%s %v", name, elapsed.Round(time.Millisecond)))
		mu.Unlock()
	}
	stop := runTestAppLoop(t, app)
	defer stop()
	before := postValue(app, func() int { return len(app.chat.Children) })

	// A streaming message that stays under D239's 8KiB off-loop threshold, so
	// every delta renders synchronously on the loop: this is the path that
	// re-highlighted a fence per delta (the attributed 100ms stall). One large Go
	// fence plus prose, delivered in many small deltas.
	body := strings.Builder{}
	body.WriteString("# streaming guide\n\n")
	body.WriteString("```go\n")
	for j := 0; j < 60; j++ {
		fmt.Fprintf(&body, "func handler%d(ctx context.Context) (string, error) { return fmt.Sprintf(\"v=%%d\", %d), nil }\n", j, j)
	}
	body.WriteString("```\n\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&body, "Paragraph %d with **bold** and `code` spans.\n\n", i)
	}
	text := body.String()
	if len(text) >= 8*1024 {
		t.Fatalf("fixture is %d bytes; the streaming path under test must stay below 8KiB", len(text))
	}
	newMessage := func(content string) *ai.AssistantMessage {
		return &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", StopReason: ai.StopPending,
			Content: ai.ContentList{ai.TextContent{Text: content}},
		}
	}
	const deltas = 60
	app.sessionEvents.enqueue(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", newMessage(""))})
	for i := 0; i < deltas; i++ {
		app.sessionEvents.enqueue(&coding.SessionEvent{Type: coding.SessionMessageUpdate,
			Agent: agentEvent("message_update", newMessage(text[:len(text)*(i+1)/deltas]))})
	}
	final := newMessage(text)
	final.StopReason = ai.StopStop
	app.sessionEvents.enqueue(&coding.SessionEvent{Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", final)})

	// Lossless per-event work: partials coalesce (latest-wins), so also drive
	// many whole messages through the reliable channel, each applied and painted.
	for i := 0; i < 60; i++ {
		user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("line %d with enough text to wrap across the viewport", i)}}
		app.sessionEvents.enqueue(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", user)})
		app.sessionEvents.enqueue(&coding.SessionEvent{Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", user)})
	}

	// The loop must apply every event: the chat grows with the streaming
	// message component and the streaming slot clears on message_end.
	waitForConditionWithin(t, func() bool {
		return postValue(app, func() bool {
			return app.transcript.StreamingComponent == nil && len(app.chat.Children) > before
		})
	}, 30*time.Second)

	mu.Lock()
	got := append([]string(nil), stalls...)
	mu.Unlock()
	if len(got) > 0 {
		t.Fatalf("main loop stalled over 100ms: %v", got)
	}
}

// The budget test relies on OnStall firing for a phase over threshold; pin that
// the hook is actually invoked so a wiring mistake cannot make it pass silently.
func TestOnStallHookFires(t *testing.T) {
	fired := make(chan string, 1)
	w := &RunWiring{
		StallLogPath:      filepath.Join(t.TempDir(), "pier-stall.log"),
		StallLogThreshold: time.Millisecond,
		OnStall: func(name string, _ time.Duration) {
			select {
			case fired <- name:
			default:
			}
		},
	}
	done := w.phase("render")
	time.Sleep(5 * time.Millisecond)
	done()
	select {
	case name := <-fired:
		if name != "render" {
			t.Fatalf("OnStall name = %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("OnStall did not fire for a phase over threshold")
	}
}
