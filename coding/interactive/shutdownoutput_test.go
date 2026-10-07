package interactive

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

type shutdownOutputTerminal struct {
	fakeRendererTerminal
	budget             time.Duration
	complete, finished bool
	hints              []string
}

func (t *shutdownOutputTerminal) BeginShutdownOutput(budget time.Duration) {
	if t.budget == 0 {
		t.budget = budget
	}
}
func (t *shutdownOutputTerminal) ShutdownOutputComplete() bool { return t.complete }
func (t *shutdownOutputTerminal) FinishShutdownOutput()        { t.finished = true }
func (t *shutdownOutputTerminal) WriteShutdownOutput(text string) bool {
	t.hints = append(t.hints, text)
	return t.complete
}

// D200: even a successful initial flush cannot make a later direct stdout
// write safe. The optional hint must use the same FIFO and remaining budget.
func TestGracefulShutdownQueuesResumeHintOnTerminal(t *testing.T) {
	terminal := &shutdownOutputTerminal{fakeRendererTerminal: fakeRendererTerminal{width: 80, height: 24}, complete: true}
	direct := false
	lifecycle := NewLifecycle(LifecycleOptions{
		UI: tui.NewMainScreen(terminal, false, ""), Terminal: terminal,
		ResumeCommand: func() string { return "resume command" },
		WriteOut:      func(string) { direct = true }, Exit: func(int) {},
	})
	lifecycle.Shutdown(false)
	if direct {
		t.Fatal("resume hint bypassed the bounded terminal FIFO")
	}
	if len(terminal.hints) != 1 || terminal.hints[0] != "resume command\n" {
		t.Fatalf("queued hints = %q", terminal.hints)
	}
}

// D200: run-context cancellation reaches Close without Lifecycle.Shutdown.
func TestCloseFinishesGracefulTerminalOutput(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	terminal := &shutdownOutputTerminal{complete: true}
	app.lifecycle.options.Terminal = terminal
	app.Close()
	if terminal.budget != 2*time.Second || !terminal.finished {
		t.Fatal("Close bypassed graceful terminal teardown")
	}
}

// D200: a terminal timeout must not be followed by a synchronous console
// write, and permanent writer admission must close before os.Exit is invoked.
func TestGracefulShutdownSkipsResumeHintAfterOutputTimeout(t *testing.T) {
	terminal := &shutdownOutputTerminal{fakeRendererTerminal: fakeRendererTerminal{width: 80, height: 24}}
	direct := false
	exited := false
	lifecycle := NewLifecycle(LifecycleOptions{
		UI: tui.NewMainScreen(terminal, false, ""), Terminal: terminal,
		ResumeCommand: func() string { return "resume command" },
		WriteOut:      func(string) { direct = true },
		Exit: func(code int) {
			exited = true
			if !terminal.finished {
				t.Error("writer admission still open at exit")
			}
		},
	})
	lifecycle.Shutdown(false)
	if direct || len(terminal.hints) != 0 {
		t.Fatal("resume hint written after terminal timeout")
	}
	if terminal.budget != 2*time.Second {
		t.Fatalf("budget = %v, want 2s", terminal.budget)
	}
	if !exited {
		t.Fatal("shutdown did not exit")
	}
}
