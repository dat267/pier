package interactive

import (
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/tui"
)

type inputDeadlineHost struct {
	fakeLoopHost
	buffer *tui.StdinBuffer
	input  []string
}

func (h *inputDeadlineHost) NextInputFlushDeadline() (time.Time, bool) {
	return h.buffer.PendingTimeout()
}

func (h *inputDeadlineHost) FlushPendingInput() {
	h.input = append(h.input, h.buffer.FlushExpired(time.Now())...)
}

// pi v1.4.0 packages/tui/src/stdin-buffer.ts gives a lone Escape its own
// timeout. D143/D145 move that deadline onto the Go UI loop; an unrelated
// idle beat must not discard it just because no component animates.
func TestSchedulePreservesPendingEscapeAcrossIdleBeats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		host := &inputDeadlineHost{buffer: tui.NewStdinBuffer(tui.StdinBufferOptions{EscapeTimeout: 10})}
		schedule := newLoopSchedule(host, func() {})
		defer schedule.close()
		host.buffer.ProcessString("\x1b")
		if schedule.arm() == nil {
			t.Fatal("Escape did not arm its input deadline")
		}
		// A session/render/resize event can wake the idle loop before Escape's
		// deadline. This is a second scheduling decision at the same fake time.
		wake := schedule.arm()
		if wake == nil {
			t.Fatal("idle beat discarded a still-pending Escape deadline")
		}
		select {
		case <-wake:
			schedule.animationFired()
			schedule.flushExpiredInput()
		case <-time.After(time.Second):
			t.Fatal("Escape deadline did not wake the loop")
		}
		if !reflect.DeepEqual(host.input, []string{"\x1b"}) {
			t.Fatalf("flushed input = %q, want one Escape", host.input)
		}
	})
}
