package tui

import (
	"strings"
	"testing"
)

// TestTrackerClearsHyperlinkOnClose covers upstream's AnsiCodeTracker.process: a
// close sequence (`\x1b]8;;\x07`) clears the active link, so later lines are not
// reopened as part of it.
func TestTrackerClearsHyperlinkOnClose(t *testing.T) {
	tracker := &ansiCodeTracker{}
	updateTrackerFromText("\x1b]8;;https://x\x07link\x1b]8;;\x07", tracker)
	if tracker.activeHyperlink != nil {
		t.Fatal("hyperlink still active after its close")
	}
	if got := tracker.activeCodes(); got != "" {
		t.Fatalf("activeCodes = %q, want empty", got)
	}
	if got := tracker.lineEndReset(); got != "" {
		t.Fatalf("lineEndReset = %q, want empty", got)
	}
}

// TestWrapClosedHyperlinkDoesNotReopen covers wrapTextWithAnsi: once an OSC 8
// link closes, the following wrapped line is plain text and the closed line
// carries exactly one close. Expectations read off the reference.
func TestWrapClosedHyperlinkDoesNotReopen(t *testing.T) {
	open := "\x1b]8;;u\x07"
	close := "\x1b]8;;\x07"
	lines := WrapTextWithAnsi(open+"abc"+close+"def", 3)
	want := []string{open + "abc" + close, "def"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

// TestTruncateClosedHyperlinkDoesNotCloseAgain covers getActiveOsc8Close: a
// prefix whose link already closed gets no extra close code.
func TestTruncateClosedHyperlinkDoesNotCloseAgain(t *testing.T) {
	open := "\x1b]8;;u\x07"
	close := "\x1b]8;;\x07"
	got := TruncateToWidth(open+"abc"+close+"defgh", 5, "", false)
	want := open + "abc" + close + "de" + "\x1b[0m"
	if got != want {
		t.Fatalf("TruncateToWidth = %q, want %q", got, want)
	}
}
