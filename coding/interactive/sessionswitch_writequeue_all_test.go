package interactive

import (
	"context"
	"testing"

	"github.com/dat267/pier/internal/offloop"
)

// A485 covers every replacement path, not only switchSession: sessionNew and
// import also build a manager through OpenSession/NewSessionManager.
func TestSessionNewKeepsTheWriteQueue(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	group := offloop.NewGroup()
	defer group.StopAll()
	queue := group.Queue()
	app.sessionMgr.SetWriteQueue(queue)

	if _, err := app.sessionNew(context.Background()); err != nil {
		t.Fatalf("sessionNew: %v", err)
	}
	if got := app.sessionMgr.GetWriteQueue(); got != queue {
		t.Fatalf("after sessionNew the write queue is %v, want %v", got, queue)
	}
}
