package tui

import (
	"strings"
	"sync"
	"testing"
)

// newProgramStatusTerminal returns a terminal whose console writes are captured. The drain
// goroutine only runs after a real Start, which a test cannot do, so snapshot() flushes the
// queue first and reads what was delivered.
func newProgramStatusTerminal(t *testing.T) (*ProcessTerminal, func() string) {
	t.Helper()
	terminal := NewProcessTerminal(nil, nil)
	var mu sync.Mutex
	var captured []string
	terminal.writeFn = func(data string) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, data)
	}
	return terminal, func() string {
		terminal.flushWrites()
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(captured, "")
	}
}

// The support query rides along with the keyboard query, with DA as the shared sentinel, and
// reports go out only after the terminal answers (upstream queryAndEnableKittyProtocol).
func TestTerminalProgramStatusQueryAndReport(t *testing.T) {
	terminal, output := newProgramStatusTerminal(t)
	terminal.setupLegacyStdinBuffer()
	status := ProgramStatus{State: ProgramStatusWorking, App: "pier"}
	terminal.SetProgramStatus(status)
	terminal.queryAndEnableKittyProtocol()

	query := output()
	if !strings.Contains(query, ProgramStatusQuery) {
		t.Fatalf("the query did not ask for program status support: %q", query)
	}
	if !strings.HasSuffix(query, deviceAttributesQuery) {
		t.Fatalf("the query did not end with the DA sentinel: %q", query)
	}
	if strings.Contains(query, FormatProgramStatus(status)) {
		t.Fatalf("a report went out before support was confirmed: %q", query)
	}

	// The reply confirms support, and the pending status goes out with it.
	terminal.FeedInput([]byte(ProgramStatusQuery))
	if got := output(); !strings.Contains(got, FormatProgramStatus(status)) {
		t.Fatalf("no report after the reply: %q", got)
	}

	// A later status is reported immediately.
	done := ProgramStatus{State: ProgramStatusDone, App: "pier"}
	terminal.SetProgramStatus(done)
	if got := output(); !strings.Contains(got, FormatProgramStatus(done)) {
		t.Fatalf("no report for a later status: %q", got)
	}

	// Stopping clears the status and forgets support.
	terminal.Stop()
	if got := output(); !strings.Contains(got, FormatProgramStatus(ProgramStatus{State: ProgramStatusClear})) {
		t.Fatalf("stopping did not clear the status: %q", got)
	}
	if terminal.programStatusSupported {
		t.Fatal("the terminal still believes it supports program status after stopping")
	}
}

// A DA sentinel that arrives first means the terminal did not answer the support query, so
// reports stay off (upstream clears the pending flag when the last owed DA arrives).
func TestTerminalProgramStatusStaysOffWhenDATakesTheQuery(t *testing.T) {
	terminal, output := newProgramStatusTerminal(t)
	terminal.setupLegacyStdinBuffer()
	status := ProgramStatus{State: ProgramStatusWorking, App: "pier"}
	terminal.SetProgramStatus(status)
	terminal.queryAndEnableKittyProtocol()

	terminal.FeedInput([]byte("\x1b[?1;2c"))
	if terminal.programStatusSupported {
		t.Fatal("a DA reply was taken as program status support")
	}
	if got := output(); strings.Contains(got, FormatProgramStatus(status)) {
		t.Fatalf("a report went out to an unsupporting terminal: %q", got)
	}
}

// PI_PROGRAM_STATUS overrides detection: 1 skips the query and reports anyway, 0 turns
// reporting off (upstream reads the same variable).
func TestTerminalProgramStatusOverride(t *testing.T) {
	t.Run("1 reports without asking", func(t *testing.T) {
		t.Setenv("PI_PROGRAM_STATUS", "1")
		terminal, output := newProgramStatusTerminal(t)
		terminal.setupLegacyStdinBuffer()
		status := ProgramStatus{State: ProgramStatusIdle, App: "pier"}
		terminal.SetProgramStatus(status)
		terminal.queryAndEnableKittyProtocol()

		got := output()
		if strings.Contains(got, ProgramStatusQuery) {
			t.Fatalf("the query was sent despite the override: %q", got)
		}
		if !strings.Contains(got, FormatProgramStatus(status)) {
			t.Fatalf("the override did not report: %q", got)
		}
	})

	t.Run("0 asks nothing and reports nothing", func(t *testing.T) {
		t.Setenv("PI_PROGRAM_STATUS", "0")
		terminal, output := newProgramStatusTerminal(t)
		terminal.setupLegacyStdinBuffer()
		terminal.SetProgramStatus(ProgramStatus{State: ProgramStatusIdle, App: "pier"})
		terminal.queryAndEnableKittyProtocol()

		got := output()
		if strings.Contains(got, ProgramStatusQuery) {
			t.Fatalf("the query was sent despite the override: %q", got)
		}
		if strings.Contains(got, "\x1b]7501;state=") {
			t.Fatalf("a report went out despite the override: %q", got)
		}
		// Even a reply cannot turn it on.
		terminal.FeedInput([]byte(ProgramStatusQuery))
		if terminal.programStatusSupported {
			t.Fatal("the override was not honoured")
		}
	})
}
