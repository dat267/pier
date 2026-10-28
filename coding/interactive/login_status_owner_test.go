package interactive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

type authStatusPostCapture struct {
	tui.TUI
	posts chan func()
}

func (c *authStatusPostCapture) Post(fn func()) { c.posts <- fn }

// pi v1.4.0 (4a080252), interactive-mode.ts login status and
// program-status-reporter.ts run on the JS owner. D156 moves only the waiting
// flow to a worker: its final status must wait for the same owner post as the
// editor restoration, not mutate ProgramStatusReporter on that worker.
func TestLoginCompletionStatusWaitsForOwnerPost(t *testing.T) {
	wiring, session := newAuthTestWiring(t)
	capture := &authStatusPostCapture{TUI: wiring.UI, posts: make(chan func(), 1)}
	wiring.UI = capture
	reporter, statuses := newProgramStatusReport(t, "fixture")
	wiring.ProgramStatus = reporter
	entered, release := make(chan struct{}), make(chan struct{})
	provider := session.runtime.GetProvider("openai")
	provider.Auth.APIKey.Login = func(*ai.AuthInteraction) (*ai.ApiKeyCredential, error) {
		close(entered)
		<-release
		return nil, errors.New("synthetic login failure")
	}
	wiring.ShowApiKeyLoginDialog(context.Background(), "openai", "OpenAI")
	<-entered
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusBlocked {
		close(release)
		t.Fatalf("login did not report blocked: %+v", got)
	}
	close(release)
	var apply func()
	select {
	case apply = <-capture.posts:
	case <-time.After(time.Second):
		t.Fatal("login did not post completion")
	}
	before := lastStatus(t, statuses())
	apply()
	if before.State != tui.ProgramStatusBlocked {
		t.Errorf("worker cleared attached program status before owner post: %+v", before)
	}
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusIdle {
		t.Errorf("owner completion did not clear blocked status: %+v", got)
	}
}
