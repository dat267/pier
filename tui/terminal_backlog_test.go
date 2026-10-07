package tui

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Pausing the console after it takes the first write makes byte counts and
// frame retention deterministic, independent of the writer's scheduling.
func TestProcessTerminalBacklogDuringPausedConsole(t *testing.T) {
	terminal := NewProcessTerminal(nil, nil)
	backlog := terminal.WriteBacklog
	pending, ok := any(terminal).(interface{ PendingWriteBytes() int64 })
	if !ok {
		t.Fatal("terminal lacks a lock-free pending-output snapshot")
	}
	release, started := make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer terminal.FlushWrites()
	defer unblock()
	var output strings.Builder
	terminal.writeFn = func(data string) {
		startOnce.Do(func() { close(started) })
		<-release
		output.WriteString(data)
	}
	terminal.Write("setup")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
	queued, inFlight, buffered := backlog()
	if queued != 0 || inFlight != 5 || buffered != 0 {
		t.Fatalf("initial backlog = (%d, %d, %d), want (0, 5, 0)", queued, inFlight, buffered)
	}
	if got := pending.PendingWriteBytes(); got != 5 {
		t.Fatalf("committed output = %d bytes, want 5 in flight", got)
	}

	// UTF-8 and ANSI sequences count as bytes, not visible cells or runes.
	const frame = "\x1b[31mα\x1b[0m"
	submitted := make(chan struct{})
	go func() {
		for i := 0; i < 64; i++ {
			terminal.BeginFrame()
			terminal.Write(frame)
			terminal.EndFrame()
		}
		terminal.Write("mode")
		close(submitted)
	}()
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("frame submissions blocked on the paused console")
	}
	queued, inFlight, buffered = backlog()
	if queued != 708 || inFlight != 5 || buffered != 0 {
		t.Fatalf("paused backlog = (%d, %d, %d), want (708, 5, 0)", queued, inFlight, buffered)
	}
	t.Logf("paused console retained 64 frames and one durable write: queued=%d bytes, in-flight=%d bytes", queued, inFlight)
	terminal.BeginFrame()
	terminal.BeginFrame()
	terminal.Write("tail")
	terminal.EndFrame()
	queued, inFlight, buffered = backlog()
	if queued != 708 || inFlight != 5 || buffered != 4 {
		t.Fatalf("open-frame backlog = (%d, %d, %d), want (708, 5, 4)", queued, inFlight, buffered)
	}
	if got := pending.PendingWriteBytes(); got != 713 {
		t.Fatalf("committed output = %d bytes, want 713 (uncommitted frame excluded)", got)
	}
	terminal.EndFrame()
	queued, inFlight, buffered = backlog()
	if queued != 712 || inFlight != 5 || buffered != 0 {
		t.Fatalf("committed backlog = (%d, %d, %d), want (712, 5, 0)", queued, inFlight, buffered)
	}
	unblock()
	terminal.FlushWrites()
	queued, inFlight, buffered = backlog()
	if queued != 0 || inFlight != 0 || buffered != 0 {
		t.Fatalf("flushed backlog = (%d, %d, %d), want (0, 0, 0)", queued, inFlight, buffered)
	}
	if got := pending.PendingWriteBytes(); got != 0 {
		t.Fatalf("flushed output = %d bytes, want zero", got)
	}
	want := "setup" + strings.Repeat(frame, 64) + "modetail"
	if got := output.String(); got != want {
		t.Fatalf("console output lost or reordered queued frames: got %q, want %q", got, want)
	}
}
