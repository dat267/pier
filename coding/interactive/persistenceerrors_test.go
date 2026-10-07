package interactive

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
)

// A failure completed during shutdown must not be mistaken for a saved
// session. Final diagnostics use D200's terminal FIFO/output grace.
func TestStopModeReportsUnresolvedSessionWriteFailure(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.Init(context.Background())
	terminal := &shutdownOutputTerminal{complete: true}
	app.lifecycle.options.Terminal = terminal
	dir := t.TempDir()
	queue := offloop.New()
	defer queue.Stop()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir, WriteQueue: queue})
	app.sessionMgr = manager
	if err := os.Mkdir(manager.GetSessionFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "save this"}})
	app.StopMode("resume-hint")
	if got := strings.Join(terminal.hints, ""); !strings.Contains(got, "session rewrite failed") {
		t.Fatalf("shutdown persistence diagnostic missing: %q", got)
	}
}

// D210: queued persistence failures are applied by the UI owner loop even
// while idle. The filesystem failure must not require another keystroke.
func TestRunLoopReportsSessionWriteFailureWhileIdle(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	defer app.Close()
	dir := t.TempDir()
	queue := offloop.New()
	defer queue.Stop()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir, WriteQueue: queue})
	app.sessionMgr = manager
	if err := os.Mkdir(manager.GetSessionFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "save this"}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	app.runner.RequestRender = func() {
		if strings.Contains(strings.Join(app.chat.Render(120), "\n"), "Session rewrite failed") {
			cancel()
		}
	}
	app.runner.runLoop(ctx, nil, nil)
	if got := strings.Join(app.chat.Render(120), "\n"); !strings.Contains(got, "Session rewrite failed") {
		t.Fatalf("idle UI did not report persistence failure: %q", got)
	}
}
