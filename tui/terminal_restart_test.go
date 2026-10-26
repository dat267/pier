package tui

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func newRestartTestTerminal(t *testing.T) (*ProcessTerminal, *os.File) {
	t.Helper()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); writer.Close() })
	terminal := NewProcessTerminal(input, nil)
	terminal.writeFn = func(string) {}
	terminal.sizeFn = func() (int, int) { return 80, 24 }
	return terminal, writer
}

// Reference: pi v1.4.0 (4a080252), terminal.ts start/stop; both renderer mode
// switches and suspend/resume restart the same terminal. D50 only drops stopped
// input, not every input after the next Start.
func TestProcessTerminalReopensRawInputAfterRestart(t *testing.T) {
	terminal, _ := newRestartTestTerminal(t)
	terminal.EnableRawInput()
	terminal.Start(func(string) {}, nil)
	if got := terminal.FeedInput([]byte("a")); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("fresh input = %q", got)
	}
	terminal.Stop()
	if got := terminal.FeedInput([]byte("stopped")); len(got) != 0 {
		t.Fatalf("stopped input = %q", got)
	}
	terminal.Start(func(string) {}, nil)
	defer terminal.Stop()
	if got := terminal.FeedInput([]byte("b")); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("restarted input = %q, want [b]", got)
	}
}

// The default library path also has to deliver into the replacement handler.
func TestProcessTerminalReopensLegacyInputAfterRestart(t *testing.T) {
	terminal, writer := newRestartTestTerminal(t)
	first := make(chan string, 1)
	terminal.Start(func(s string) { first <- s }, nil)
	if _, err := writer.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-first:
		if got != "a" {
			t.Fatalf("fresh input = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("fresh input was not delivered")
	}
	terminal.Stop()
	second := make(chan string, 1)
	terminal.Start(func(s string) { second <- s }, nil)
	defer terminal.Stop()
	if _, err := writer.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-second:
		if got != "b" {
			t.Fatalf("restarted input = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("restarted input was not delivered")
	}
}

// A callback may stop/start the terminal. Its old partial sequence must not
// contaminate the replacement buffer, and no retained reader lock may deadlock it.
func TestProcessTerminalCanRestartFromLegacyInputCallback(t *testing.T) {
	terminal, writer := newRestartTestTerminal(t)
	restarted := make(chan struct{})
	second := make(chan string, 1)
	terminal.Start(func(string) {
		terminal.Stop()
		terminal.Start(func(s string) { second <- s }, nil)
		close(restarted)
	}, nil)
	defer terminal.Stop()
	if _, err := writer.Write([]byte("a\x1b[")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("restarting from the input callback deadlocked")
	}
	if _, err := writer.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-second:
		if got != "b" {
			t.Fatalf("new input inherited the old partial sequence: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("new handler did not receive input")
	}
}
