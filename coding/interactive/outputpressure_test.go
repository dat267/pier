package interactive

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// The real ProcessTerminal writes into a pipe with no reader until released.
// This exercises D198 through the input loop, frame batching and async writer,
// rather than relying only on a fake backlog value in scheduling tests.
func TestRunLoopWithBlockedConsole(t *testing.T) {
	for _, cancelWhileBlocked := range []bool{false, true} {
		name := "resume"
		if cancelWhileBlocked {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("PI_TUI_WRITE_LOG", "")
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			terminal := tui.NewProcessTerminal(nil, writer)
			wiring, _ := newRunTestWiring(t)
			dispatcher, _, _, _ := newEventTestDispatcher(t)
			queue := newSessionEventQueue()
			defer queue.Close()
			wiring.Terminal = terminal
			wiring.Events = dispatcher
			wiring.SessionEvents = queue.Events()
			wiring.PartialEvents = queue.Partials()
			inputs := make(chan string, 128)
			wiring.InputEvents = inputs
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queue.SetContext(ctx)
			screen := wiring.UI.(*tui.MainScreen)
			screen.Terminal = terminal
			typed, frames := "", 0
			type progress struct {
				text   string
				frames int
				bytes  int64
			}
			checked := make(chan progress, 1)
			painted := make(chan string, 4)
			screen.AddInputListener(func(data string) tui.TuiInputListenerResult {
				if data == "check" {
					checked <- progress{typed, frames, terminal.PendingWriteBytes()}
				} else {
					typed += data
				}
				screen.RequestRender(false)
				return tui.TuiInputListenerResult{Consume: true}
			})
			screen.DoRender = func() {
				frames++
				terminal.Write("frame:" + typed)
				select {
				case painted <- typed:
				default:
				}
			}
			settled := make(chan struct{}, 1)
			dispatcher.CheckShutdownRequested = func() { settled <- struct{}{} }
			prefix := strings.Repeat("p", 512*1024)
			terminal.Write(prefix)
			terminal.Write("mode") // durable writes remain lossless while paused
			done := make(chan struct{})
			go func() { defer close(done); wiring.runLoop(ctx, nil, nil) }()
			defer func() {
				cancel()
				reader.Close() // release a blocked syscall on any failure path
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("loop did not stop")
				}
				terminal.FlushWrites()
			}()
			queue.enqueue(&coding.SessionEvent{Type: coding.SessionAgentSettled})
			select {
			case <-settled:
			case <-time.After(2 * time.Second):
				t.Fatal("session events stopped draining while the console was blocked")
			}
			for i := 0; i < 64; i++ {
				inputs <- "x"
			}
			inputs <- "check"
			select {
			case snapshot := <-checked:
				if snapshot.text != strings.Repeat("x", 64) || snapshot.frames != 0 || snapshot.bytes != int64(len(prefix)+4) {
					t.Fatalf("blocked-console progress = %+v, want 64 inputs, no frames and %d output bytes", snapshot, len(prefix)+4)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("input dispatch stalled behind the console")
			}
			if cancelWhileBlocked {
				cancel()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("cancellation stalled behind the console")
				}
				return
			}
			output := make(chan string, 1)
			go func() { data, _ := io.ReadAll(reader); output <- string(data) }()
			select {
			case text := <-painted:
				if text != strings.Repeat("x", 64) {
					t.Fatalf("recovery painted stale content: %q", text)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("deferred paint did not recover without another request")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("loop did not stop after recovery")
			}
			terminal.FlushWrites()
			writer.Close()
			select {
			case got := <-output:
				want := prefix + "modeframe:" + strings.Repeat("x", 64)
				if got != want {
					t.Fatal("recovery dropped/reordered committed output or generated extra frames")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("console output did not finish draining")
			}
		})
	}
}
