package interactive

import (
	"context"
	"testing"
	"time"
)

// Reference: pi v1.5.0 (f9a26365), agent-session-runtime.ts teardownCurrent
// awaits abort without blocking the JS event loop. D143's bounded Go handoff
// must retire the outgoing producer before waiting for that run to settle.
func TestSessionReplacementSettlesAProducerParkedOnTheFullEventQueue(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "no backpressure"
		if full {
			name = "full lossless queue"
		}
		t.Run(name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.Init(context.Background())
			outgoing := app.session.AgentSession
			ready := make(chan struct{})
			outgoing.SetBeforeFirstTurn(func(ctx context.Context) {
				if full {
					for range sessionEventLosslessCapacity {
						outgoing.ClearQueue() // A real public producer of lossless events.
					}
					close(ready)
					outgoing.ClearQueue() // Park the active run on event backpressure.
					return
				}
				close(ready)
				<-ctx.Done()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			promptDone := make(chan error, 1)
			go func() { promptDone <- outgoing.Prompt(ctx, "start", nil) }()
			select {
			case <-ready:
			case <-ctx.Done():
				t.Fatal("prompt did not reach the producer barrier")
			}

			replaced := make(chan error, 1)
			go func() {
				_, err := app.sessionNew(context.Background())
				replaced <- err
			}()
			stalled := false
			select {
			case err := <-replaced:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				stalled = true
				// Release the old implementation's blocked worker before reporting
				// the failure, rather than leaving a deadlocked test goroutine.
				app.sessionEvents.Close()
				select {
				case <-replaced:
				case <-ctx.Done():
					t.Fatal("replacement stayed blocked after emergency queue close")
				}
			}
			select {
			case <-promptDone:
			case <-ctx.Done():
				t.Fatal("outgoing prompt did not settle")
			}
			if stalled {
				t.Fatal("session replacement blocked waiting for a producer on its own undrained event queue")
			}
		})
	}
}
