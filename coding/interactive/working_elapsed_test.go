package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

func renderedText(lines []string) string {
	return tui.StripTerminalSequences(strings.Join(lines, "\n"))
}

// D402: pier addition (pi v1.5.0 status-indicator.ts shows only the
// message). The working indicator shows the time since the agent started,
// across every indicator the run shows, and the transcript notes how long the
// run worked once it ends.
func TestWorkingIndicatorShowsElapsedAndTranscriptNotesTheRun(t *testing.T) {
	dispatcher, _, transcript, uiState := newEventTestDispatcher(t)
	started := time.Now().Add(-75 * time.Second)
	saved := workingClock
	workingClock = func() time.Time { return started }
	t.Cleanup(func() { workingClock = saved })

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionTurnStart})
	working, ok := uiState.ActiveStatusIndicator.(*StatusIndicator)
	if !ok || working.Kind != StatusWorking {
		t.Fatalf("active indicator = %#v", uiState.ActiveStatusIndicator)
	}
	if got := renderedText(working.Render(80)); !strings.Contains(got, "Working (1m 15s)") {
		t.Fatalf("working indicator = %q, want the elapsed time", got)
	}

	// A retry replaces the indicator; the working indicator after it keeps
	// counting from the agent start, not from its own creation.
	uiState.ClearStatusIndicator(StatusWorking, true)
	uiState.ShowWorkingStatusIndicator("")
	working = uiState.ActiveStatusIndicator.(*StatusIndicator)
	if got := renderedText(working.Render(80)); !strings.Contains(got, "Working (1m 15s)") {
		t.Fatalf("re-shown working indicator = %q", got)
	}

	workingClock = func() time.Time { return started.Add(83*time.Second + 400*time.Millisecond) }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
	if got := renderedText(transcript.Chat.Render(80)); !strings.Contains(got, "Worked for 1m 23s") {
		t.Fatalf("transcript = %q, want the worked-for notice", got)
	}
	// The notice is its own line: a later status does not replace it.
	transcript.ShowStatus("Model switched")
	if got := renderedText(transcript.Chat.Render(80)); !strings.Contains(got, "Worked for 1m 23s") || !strings.Contains(got, "Model switched") {
		t.Fatalf("transcript after a status = %q", got)
	}
	// An agent end without a start (a replayed event) adds nothing.
	before := len(transcript.Chat.Children)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
	if len(transcript.Chat.Children) != before {
		t.Fatal("an agent end without a running clock added a notice")
	}
}

func TestWorkingElapsedLabelFormats(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    string
	}{
		{0, "(0s)"},
		{999 * time.Millisecond, "(0s)"},
		{59 * time.Second, "(59s)"},
		{65 * time.Second, "(1m 5s)"},
		{3723 * time.Second, "(1h 2m 3s)"},
	} {
		if got := workingElapsedLabel(tc.elapsed); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.elapsed, got, tc.want)
		}
	}
}
