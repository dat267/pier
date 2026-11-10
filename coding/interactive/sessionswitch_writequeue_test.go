package interactive

import (
	"context"
	"testing"

	"github.com/dat267/pier/internal/offloop"
)

// A485: a session switch must keep the write queue. OpenSession builds a
// manager with no queue, so without forwarding it the new session writes its
// file synchronously on the calling goroutine — for the interactive path the
// owner loop. cmd/main.go:343-351 wires the queue for exactly this reason and
// sessionNew forwards it explicitly; switchSession did not.
//
// Invariant: ARCHITECTURE.md, "nothing on the main event loop may block", and
// SessionManagerOptions.WriteQueue, which documents nil as "one-shot
// consumers" only.
func TestSwitchSessionKeepsTheWriteQueue(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	group := offloop.NewGroup()
	defer group.StopAll()
	queue := group.Queue()
	app.sessionMgr.SetWriteQueue(queue)
	if app.sessionMgr.GetWriteQueue() != queue {
		t.Fatal("fixture did not install the queue on the starting manager")
	}

	target := makePersistedSession(t, t.TempDir(), "other session")
	if _, err := app.switchSession(context.Background(), target.GetSessionFile(), ""); err != nil {
		t.Fatalf("switchSession: %v", err)
	}
	if got := app.sessionMgr.GetWriteQueue(); got != queue {
		t.Fatalf("after switching, the session manager's write queue is %v, want the previous queue %v (nil means synchronous writes on the owner loop)", got, queue)
	}
}
