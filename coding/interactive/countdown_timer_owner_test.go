package interactive

import (
	"testing"
	"time"
)

// pi v1.4.0 packages/coding-agent/src/modes/interactive/components/countdown-timer.ts
// runs interval callbacks on the TUI event loop; ticks must reach the UI owner,
// and queued callbacks must be ignored after disposal.
type countdownPostTestHost struct {
	posts chan func()
}

func (h *countdownPostTestHost) Post(fn func()) { h.posts <- fn }

func TestCountdownTimerPostsTicksToUIOwner(t *testing.T) {
	host := &countdownPostTestHost{posts: make(chan func(), 1)}
	ticks := make(chan int, 2)
	timer := NewCountdownTimer(3000, host, func(seconds int) { ticks <- seconds }, nil)
	defer timer.Dispose()

	if got := <-ticks; got != 3 {
		t.Fatalf("initial tick = %d, want 3", got)
	}

	select {
	case got := <-ticks:
		t.Fatalf("periodic tick %d ran on the timer goroutine instead of being posted", got)
	case fn := <-host.posts:
		fn()
		if got := <-ticks; got != 2 {
			t.Fatalf("posted tick = %d, want 2", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("countdown did not deliver its first periodic tick")
	}
}

func TestCountdownTimerDisposeSuppressesQueuedTick(t *testing.T) {
	host := &countdownPostTestHost{posts: make(chan func(), 1)}
	ticks := make(chan int, 2)
	timer := NewCountdownTimer(3000, host, func(seconds int) { ticks <- seconds }, nil)
	defer timer.Dispose()

	if got := <-ticks; got != 3 {
		t.Fatalf("initial tick = %d, want 3", got)
	}

	select {
	case got := <-ticks:
		t.Fatalf("periodic tick %d ran before its owner post", got)
	case fn := <-host.posts:
		timer.Dispose()
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("countdown did not post its first periodic tick")
	}

	select {
	case got := <-ticks:
		t.Fatalf("queued tick %d ran after disposal", got)
	default:
	}
}
