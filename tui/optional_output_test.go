package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// D202 does not truncate existing titles. A single oversized title may exceed
// the watermark, like a large atomic differential frame, but must still drain.
func TestOversizedTitleMakesProgressOnIdleTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := NewProcessTerminal(nil, nil)
		var output strings.Builder
		terminal.writeFn = func(data string) { output.WriteString(data) }
		defer terminal.FinishShutdownOutput()
		title := strings.Repeat("t", 256*1024)
		terminal.SetTitle(title)
		synctest.Wait()
		if output.String() != "\x1b]0;"+title+"\x07" {
			t.Fatal("oversized title never reached idle writer")
		}
	})
}

// D202: title and progress refreshes are redundant metadata, not display
// diffs. Backpressure keeps only newest uncommitted hints; progress clear
// remains essential and invalidates a delayed active hint.
func TestOptionalMetadataDoesNotGrowBlockedConsoleFIFO(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := NewProcessTerminal(nil, nil)
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var output strings.Builder
		terminal.writeFn = func(data string) { <-release; output.WriteString(data) }
		defer func() {
			terminal.SetProgress(false)
			terminal.FinishShutdownOutput()
			unblock()
			_ = terminal.FlushWritesContext(context.Background())
		}()
		prefix := strings.Repeat("x", 256*1024)
		terminal.Write(prefix)
		synctest.Wait()
		for i := 0; i < 64; i++ {
			terminal.SetTitle("title" + strconv.Itoa(i))
			terminal.SetProgress(true)
		}
		time.Sleep(10 * time.Second) // virtual keepalive ticks, never a wall sleep
		synctest.Wait()
		queued, inFlight, _ := terminal.WriteBacklog()
		t.Logf("64 title/progress refreshes plus 10 keepalives: queued=%d inFlight=%d", queued, inFlight)
		if queued != 0 || inFlight != len(prefix) {
			t.Fatal("optional metadata grew committed output behind blocked console")
		}
		terminal.SetProgress(false)
		unblock()
		terminal.FlushWrites()
		want := prefix + terminalProgressClearSequence + "\x1b]0;title63\x07"
		if output.String() != want {
			t.Fatal("recovery lost the newest title or replayed stale progress after clear")
		}
	})
}

// D202: admission after recovery is atomic with respect to an open frame.
// The producer must not splice its OSC/text packet into renderer output.
func TestOptionalOutputPreservesFrameAndFIFOOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := NewProcessTerminal(nil, nil)
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var output strings.Builder
		terminal.writeFn = func(data string) { <-release; output.WriteString(data) }
		defer func() { terminal.FinishShutdownOutput(); unblock(); terminal.FlushWrites() }()
		prefix := strings.Repeat("x", 256*1024)
		terminal.Write(prefix)
		synctest.Wait()
		done := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { done <- terminal.WriteOptionalContext(ctx, "optional") }()
		terminal.BeginFrame()
		terminal.Write("frame")
		unblock()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("optional output entered an open frame")
		default:
		}
		terminal.Write("tail")
		terminal.EndFrame()
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		terminal.FlushWrites()
		if output.String() != prefix+"frametailoptional" {
			t.Fatal("output was split or reordered")
		}
	})
}

// D202: optional off-loop output waits before admission, not in a console
// syscall. Essential UI writes still enqueue, and cancellation drops no frame.
func TestOptionalOutputWaitsForCapacityWithoutBlockingEssentialWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := NewProcessTerminal(nil, nil)
		optional, ok := any(terminal).(interface {
			WriteOptionalContext(context.Context, string) error
		})
		if !ok {
			t.Fatal("terminal has no cancellation-aware optional admission")
		}
		release := make(chan struct{})
		var output strings.Builder
		terminal.writeFn = func(data string) { <-release; output.WriteString(data) }
		defer func() {
			terminal.FinishShutdownOutput()
			close(release)
			_ = terminal.FlushWritesContext(context.Background())
		}()
		prefix := strings.Repeat("x", 256*1024)
		terminal.Write(prefix)
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- optional.WriteOptionalContext(ctx, "optional") }()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("optional output bypassed capacity")
		default:
		}
		terminal.ShowCursor()
		queued, inFlight, _ := terminal.WriteBacklog()
		if queued != len("\x1b[?25h") || inFlight != len(prefix) {
			t.Fatalf("backlog=(%d,%d), optional bytes were admitted", queued, inFlight)
		}
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("optional result=%v", err)
			}
		default:
			t.Fatal("cancellation did not release optional producer")
		}
		queued, inFlight, _ = terminal.WriteBacklog()
		if queued+inFlight != len(prefix)+len("\x1b[?25h") {
			t.Fatal("cancellation changed committed backlog")
		}
	})
}
