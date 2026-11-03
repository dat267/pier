package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// Reference: pi v1.5.0 (f9a26365), interactive-mode.ts
// rebindCurrentSession replaces the outgoing event binding. D143's Go handoff
// must reject a listener snapshot that resumes after that binding is retired.
func TestSessionReplacementRejectsAnAlreadyAdmittedOldListener(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	// This subscriber precedes the app's subscription. AgentSession.emit has
	// already snapshotted both callbacks when the first one pauses delivery.
	app.session.Subscribe(func(event *coding.SessionEvent) {
		if event.Type == coding.SessionMessageStart {
			close(entered)
			<-release
		}
	})
	app.Init(context.Background())
	outgoing := app.session.AgentSession
	produced := make(chan struct{})
	go func() {
		defer close(produced)
		outgoing.AppendCustomMessage(&ai.CustomMessage{
			Role:    coding.RoleCustom,
			Content: json.RawMessage(`{"customType":"note","content":"late outgoing note","display":true}`),
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old event did not reach its admitted listener")
	}
	if _, err := app.sessionNew(context.Background()); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-produced:
	case <-time.After(5 * time.Second):
		t.Fatal("old event producer did not finish")
	}
	app.runner.drainReadyEvents()
	chat := coding.StripAnsi(strings.Join(app.chat.Render(120), "\n"))
	if strings.Contains(chat, "late outgoing note") {
		t.Fatalf("an admitted outgoing listener contaminated the replacement transcript:\n%s", chat)
	}
}
