package interactive

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/coding"
)

// Reference: pi v1.5.0 (f9a26365), interactive-mode.ts
// rebindCurrentSession/subscribeToAgent keep UI events on the same JS owner.
// D143 requires the Go replacement subscription to enqueue like initial Init.
func TestSessionReplacementStillQueuesEventsForTheUIOwner(t *testing.T) {
	for _, mode := range []string{"initial session", "new session", "switched session"} {
		t.Run(mode, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.Init(context.Background())
			switch mode {
			case "new session":
				if _, err := app.sessionNew(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "switched session":
				target := makePersistedSession(t, t.TempDir(), "incoming session")
				if _, err := app.switchSession(context.Background(), target.GetSessionFile(), ""); err != nil {
					t.Fatal(err)
				}
			}

			var titleUpdates atomic.Int64
			app.events.UpdateTerminalTitle = func() { titleUpdates.Add(1) }
			produced := make(chan struct{})
			go func() {
				defer close(produced)
				app.session.SetSessionName("named by a worker")
			}()
			<-produced
			if got := titleUpdates.Load(); got != 0 {
				t.Fatalf("worker applied %d title updates before the UI owner drained events", got)
			}
			select {
			case event := <-app.sessionEvents.Events():
				if event.Type != coding.SessionInfoChanged {
					t.Fatalf("queued event = %s, want session_info_changed", event.Type)
				}
				app.events.handleQueuedEvent(event)
			default:
				t.Fatal("session event was not queued for the UI owner")
			}
			if got := titleUpdates.Load(); got != 1 {
				t.Fatalf("owner applied %d title updates, want 1", got)
			}
		})
	}
}
