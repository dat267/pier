package tui

import (
	"os"
	"testing"
	"time"
)

// D50: a stopped terminal's stdin read cannot be interrupted, so it stays in
// Read until input arrives. A screen that hands the terminal to another
// ProcessTerminal on the same input (the startup trust prompt or the --resume
// picker, then the app) must not leave that read competing with the next
// terminal's reader. Reproduction: after the project-trust prompt, the app's
// terminal-color replies were the first input, the startup screen's leftover
// reader won the read and discarded it, and the system theme never received
// the terminal colors. Node has one process.stdin stream whose data events go to
// whichever listener is attached (pi v1.5.0 f9a26365, packages/tui/src/
// terminal.ts start/stop), so the reply reaches the app there.
func TestStdinHandoffBetweenTerminalsDeliversToTheStartedOne(t *testing.T) {
	for attempt := range 20 {
		inR, inW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		outR, outW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			buf := make([]byte, 4096)
			for {
				if _, err := outR.Read(buf); err != nil {
					return
				}
			}
		}()

		first := NewProcessTerminal(inR, outW)
		first.EnableRawInput()
		gotFirst := make(chan string, 4)
		first.Start(func(data string) { gotFirst <- data }, nil)
		// Let the first reader block in Read, as it does behind a prompt.
		time.Sleep(5 * time.Millisecond)
		first.Stop()

		second := NewProcessTerminal(inR, outW)
		second.EnableRawInput()
		gotSecond := make(chan string, 4)
		second.Start(func(data string) { gotSecond <- data }, nil)

		if _, err := inW.Write([]byte("reply")); err != nil {
			t.Fatal(err)
		}
		select {
		case data := <-gotSecond:
			if data != "reply" {
				t.Fatalf("attempt %d: second terminal got %q", attempt, data)
			}
		case data := <-gotFirst:
			t.Fatalf("attempt %d: the stopped terminal received %q", attempt, data)
		case <-time.After(2 * time.Second):
			t.Fatalf("attempt %d: the started terminal never received the input", attempt)
		}
		second.Stop()
		inW.Close()
		inR.Close()
		outW.Close()
		outR.Close()
	}
}
