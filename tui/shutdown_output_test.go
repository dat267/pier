package tui

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// D200: finalized output cannot claim later writes were delivered, even when
// the caller never opened a grace period (for example a never-started terminal).
func TestFinishedShutdownOutputRejectsHints(t *testing.T) {
	terminal := NewProcessTerminal(nil, nil)
	terminal.FinishShutdownOutput()
	if terminal.ShutdownOutputComplete() || terminal.WriteShutdownOutput("late") {
		t.Fatal("finalized terminal accepted a shutdown hint")
	}
}

// D200: the final hint shares the original deadline and never bypasses FIFO.
func TestShutdownHintSharesBudgetAndPreservesFIFO(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := NewProcessTerminal(nil, nil)
		writer, ok := any(terminal).(interface{ WriteShutdownOutput(string) bool })
		if !ok {
			t.Fatal("shutdown hint has no bounded FIFO writer")
		}
		release := make(chan struct{})
		var output strings.Builder
		terminal.writeFn = func(data string) { <-release; output.WriteString(data) }
		defer func() {
			terminal.FinishShutdownOutput()
			terminal.Write("late") // admission closed, not part of committed output
			close(release)
			_ = terminal.FlushWritesContext(context.Background())
			if output.String() != "framehint" {
				t.Errorf("output = %q, want framehint", output.String())
			}
		}()
		terminal.BeginShutdownOutput(2 * time.Second)
		terminal.Write("frame")
		time.Sleep(1500 * time.Millisecond)
		terminal.BeginShutdownOutput(2 * time.Second) // must not extend the budget
		done := make(chan bool, 1)
		go func() { done <- writer.WriteShutdownOutput("hint") }()
		time.Sleep(499 * time.Millisecond)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("hint returned before remaining grace elapsed")
		default:
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		select {
		case success := <-done:
			if success {
				t.Fatal("blocked hint reported delivery")
			}
		default:
			t.Fatal("hint extended the original deadline")
		}
	})
}

// D200: graceful exit bounds only terminal delivery, not accepted saves.
// A stalled console retains its FIFO while Stop releases the caller at 2 s.
func TestShutdownOutputBoundsStopWithoutDroppingQueuedBytes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := NewProcessTerminal(nil, nil)
		shutdown, ok := any(terminal).(interface {
			BeginShutdownOutput(time.Duration)
			ShutdownOutputComplete() bool
			FinishShutdownOutput()
			FlushWritesContext(context.Context) error
		})
		if !ok {
			t.Fatal("terminal has no bounded graceful-shutdown policy")
		}
		release := make(chan struct{})
		var output strings.Builder
		terminal.writeFn = func(data string) { <-release; output.WriteString(data) }
		defer func() {
			shutdown.FinishShutdownOutput()
			close(release)
			_ = shutdown.FlushWritesContext(context.Background())
			if output.String() != "committed\x1b[?2004l" {
				t.Errorf("recovered output = %q, want committed bytes then protocol cleanup", output.String())
			}
		}()
		terminal.Write("committed")
		synctest.Wait()
		shutdown.BeginShutdownOutput(2 * time.Second)
		stopped := make(chan struct{})
		go func() { terminal.Stop(); close(stopped) }()
		time.Sleep(1999 * time.Millisecond)
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("Stop returned before the 2 s grace elapsed")
		default:
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("Stop waited beyond the 2 s grace")
		}
		if shutdown.ShutdownOutputComplete() {
			t.Fatal("blocked console reported successful delivery")
		}
		queued, inFlight, _ := terminal.WriteBacklog()
		if queued+inFlight != len("committed\x1b[?2004l") {
			t.Fatalf("retained bytes = %d, want complete FIFO", queued+inFlight)
		}
	})
}
