package interactive

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

type startupFloodTerminal struct {
	*fakeRendererTerminal
	filled chan struct{}
	done   chan struct{}
}

func (terminal *startupFloodTerminal) Start(onInput func(string), _ func()) {
	go func() {
		// Fill the startup loop's 256-slot input queue while its initial
		// paint is held. The next admitted callback must block until retired.
		for range 256 {
			onInput("x")
		}
		close(terminal.filled)
		onInput("last input")
		close(terminal.done)
	}()
}

type startupSettlingFrame struct {
	filled  <-chan struct{}
	settled bool
}

func (frame *startupSettlingFrame) Render(int) []string {
	<-frame.filled
	frame.settled = true
	return []string{"ready"}
}
func (*startupSettlingFrame) Invalidate() {}

// Reference: pi f9a26365 cli/startup-ui.ts hands the terminal to the next
// screen after selection. D143's Go input handoff must retire admitted sends
// when that startup owner exits, including sends parked on a full queue.
func TestStartupLoopReleasesBlockedInputProducer(t *testing.T) {
	const probeEnv = "PIER_TEST_STARTUP_INPUT_RETIREMENT"
	if os.Getenv(probeEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupLoopReleasesBlockedInputProducer$")
		command.Env = append(os.Environ(), probeEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("startup input handoff probe: %v\n%s", err, output)
		}
		return
	}

	terminal := &startupFloodTerminal{fakeRendererTerminal: &fakeRendererTerminal{width: 80, height: 24},
		filled: make(chan struct{}), done: make(chan struct{})}
	screen := tui.NewMainScreen(terminal, false, "")
	frame := &startupSettlingFrame{filled: terminal.filled}
	screen.AddChild(frame)
	RunStartupScreenLoop(screen, terminal, func() bool { return frame.settled })
	select {
	case <-terminal.done:
	case <-time.After(time.Second):
		// The probe process owns the blocked producer, so a failed assertion
		// cannot leave a leaked input goroutine in the main test process.
		t.Fatal("startup exited with its admitted input producer still blocked")
	}
}
