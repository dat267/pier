package ai

import (
	"context"
	"io"
	"testing"
	"time"
)

type closingReadBody struct {
	onRead func()
}

func (b *closingReadBody) Read(p []byte) (int, error) {
	b.onRead()
	p[0] = 'x'
	return 1, io.EOF
}

func (b *closingReadBody) Close() error { return nil }

// Reference: pi v1.4.0 core/http-dispatcher.ts's bodyTimeout; D254 implements
// it with a Go timer. A pending read may return buffered bytes after Close,
// but must not resurrect the closed response's timer and retain it forever.
func TestIdleGuardDoesNotRearmAfterCloseDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := newIdleBody(context.Background(), cancel, time.Hour)
	body.gotHeaders.Store(true)
	body.rc = &closingReadBody{onRead: func() { _ = body.Close() }}
	defer body.Close()
	if n, err := body.Read(make([]byte, 1)); n != 1 || err != io.EOF {
		t.Fatalf("final buffered read = %d, %v", n, err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("response close did not release its request context")
	}
	if body.timer.Stop() {
		t.Fatal("read completion rearmed the closed response's idle timer")
	}
}
