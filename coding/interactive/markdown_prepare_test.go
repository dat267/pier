package interactive

import (
	"context"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppLargeMarkdownUsesOwnedBoundedQueue(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); app.markdownQueue.Flush() }()
	app.markdownQueue.GoContext(func(context.Context) { <-release })
	message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: strings.Repeat("# Heading\n\nparagraph\n\n", 4000)}}}
	app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", message)})
	if lines := app.chat.Render(80); !strings.Contains(strings.Join(lines, "\n"), "Preparing message...") {
		t.Fatal("app did not defer large render")
	}
	queued, running := app.markdownQueue.Backlog()
	if queued+running != 2 {
		t.Fatalf("markdown admission=%d, want blocker plus one render", queued+running)
	}
	unblock()
	app.markdownQueue.Flush()
	app.ui.RenderNow(false)
	if strings.Contains(strings.Join(app.chat.Render(80), "\n"), "Preparing message...") {
		t.Fatal("owner did not apply completed preparation")
	}
}

func TestInputContinuesWhileLargeMarkdownPreparationIsBlocked(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	app.markdownQueue.GoContext(func(context.Context) { <-release })
	stop := startLoopApp(t, app)
	defer stop()
	defer unblock()
	posted := make(chan struct{})
	app.ui.Post(func() {
		message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: strings.Repeat("# Heading\n\nparagraph\n\n", 4000)}}}
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", message)})
		app.chat.Render(80)
		close(posted)
	})
	select {
	case <-posted:
	case <-time.After(2 * time.Second):
		t.Fatal("message preparation trapped owner")
	}
	app.postTerminalInput("responsive")
	waitForConditionWithin(t, func() bool { return strings.Contains(editorText(app), "responsive") }, 2*time.Second)
}

// D204: live assistant messages, not only detached session prerenders, opt in.
func TestStreamingAssistantUsesDetachedMarkdownPreparation(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	var work func() func()
	app.events.MarkdownPreparation = &tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }, RequestRender: func() { app.chat.MarkDirty() }}
	message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: strings.Repeat("# Heading\n\nparagraph\n\n", 4000)}}}
	app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", message)})
	lines := app.chat.Render(80)
	if work == nil || !strings.Contains(strings.Join(lines, "\n"), "Preparing message...") {
		t.Fatal("streaming message rendered synchronously")
	}
	apply := work()
	apply()
	if strings.Contains(strings.Join(app.chat.Render(80), "\n"), "Preparing message...") {
		t.Fatal("prepared live frame never replaced pending display")
	}
}
