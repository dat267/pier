package interactive

import (
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Cases mirroring packages/coding-agent/test/program-status-reporter.test.ts.
func newProgramStatusReport(t *testing.T, sessionName string) (*ProgramStatusReporter, func() []tui.ProgramStatus) {
	t.Helper()
	terminal := &fakeRendererTerminal{width: 80, height: 24}
	reporter := NewProgramStatusReporter(
		func() tui.Terminal { return terminal },
		func() string { return "pier" },
		func() string { return sessionName },
	)
	return reporter, func() []tui.ProgramStatus {
		out := make([]tui.ProgramStatus, len(terminal.programStatuses))
		copy(out, terminal.programStatuses)
		return out
	}
}

func lastStatus(t *testing.T, statuses []tui.ProgramStatus) tui.ProgramStatus {
	t.Helper()
	if len(statuses) == 0 {
		t.Fatal("no status was reported")
	}
	return statuses[len(statuses)-1]
}

func messageEndEvent(assistant *ai.AssistantMessage) *coding.SessionEvent {
	return &coding.SessionEvent{Type: coding.SessionMessageEnd, Agent: &agent.AgentEvent{Message: assistant}}
}

func TestProgramStatusReporterRunLifecycle(t *testing.T) {
	reporter, statuses := newProgramStatusReport(t, "my session")
	reporter.Report()
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusIdle || got.App != "pier" {
		t.Fatalf("initial status = %+v", got)
	}

	// A run reports working with the session name.
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	got := lastStatus(t, statuses())
	if got.State != tui.ProgramStatusWorking || got.Message != "my session" {
		t.Fatalf("working status = %+v", got)
	}
	// The same status is not reported twice.
	before := len(statuses())
	reporter.Report()
	if len(statuses()) != before {
		t.Fatalf("a repeated status was reported again: %d reports", len(statuses()))
	}

	// A settled run without an error reports done.
	reporter.HandleEvent(messageEndEvent(&ai.AssistantMessage{StopReason: ai.StopStop}))
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusDone || got.Message != "my session" {
		t.Fatalf("done status = %+v", got)
	}

	// A cancelled run reports idle instead of the run's outcome.
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled, Aborted: true})
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusIdle {
		t.Fatalf("aborted status = %+v", got)
	}
}

func TestProgramStatusReporterReportsErrorsByFirstLine(t *testing.T) {
	reporter, statuses := newProgramStatusReport(t, "s")
	message := "Provider exploded\nstack trace"
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	reporter.HandleEvent(messageEndEvent(&ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: &message}))
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})
	got := lastStatus(t, statuses())
	if got.State != tui.ProgramStatusError || got.Message != "Provider exploded" {
		t.Fatalf("error status = %+v", got)
	}
}

func TestProgramStatusReporterCompaction(t *testing.T) {
	reporter, statuses := newProgramStatusReport(t, "s")
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionStart})
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusWorking || got.Message != "Compacting context" {
		t.Fatalf("compaction status = %+v", got)
	}
	// A manual compaction that succeeded reports done even with no run active.
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionEnd, Reason: coding.CompactionManual})
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusDone {
		t.Fatalf("compaction done status = %+v", got)
	}
	// A failed one reports its error.
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionStart})
	reporter.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionCompactionEnd, Reason: coding.CompactionManual, ErrorMessage: "no room\ndetails",
	})
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusError || got.Message != "no room" {
		t.Fatalf("failed compaction status = %+v", got)
	}
}

func TestProgramStatusReporterBlockedDialogs(t *testing.T) {
	reporter, statuses := newProgramStatusReport(t, "s")
	reporter.Report()

	auth := BlockedStatus{Kind: tui.ProgramStatusKindAuth, Message: "Log in to Anthropic"}
	reporter.SetBlocked("login", &auth)
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusBlocked ||
		got.Kind != tui.ProgramStatusKindAuth || got.Message != "Log in to Anthropic" {
		t.Fatalf("blocked status = %+v", got)
	}

	// The most recent dialog reports; clearing it returns to the one below.
	question := BlockedStatus{Kind: tui.ProgramStatusKindQuestion, Message: "Which model?"}
	reporter.SetBlocked("dialog", &question)
	if got := lastStatus(t, statuses()); got.Message != "Which model?" {
		t.Fatalf("newest dialog did not report: %+v", got)
	}
	reporter.SetBlocked("dialog", nil)
	if got := lastStatus(t, statuses()); got.Message != "Log in to Anthropic" {
		t.Fatalf("clearing did not fall back to the older dialog: %+v", got)
	}
	reporter.SetBlocked("login", nil)
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusIdle {
		t.Fatalf("clearing every dialog did not return to idle: %+v", got)
	}

	// Reopening a source replaces its status instead of stacking.
	reporter.SetBlocked("login", &auth)
	reporter.SetBlocked("login", &question)
	reporter.SetBlocked("login", nil)
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusIdle {
		t.Fatalf("a replaced dialog stacked: %+v", got)
	}
}

func TestProgramStatusReporterReset(t *testing.T) {
	reporter, statuses := newProgramStatusReport(t, "s")
	reporter.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	reporter.Reset()
	got := lastStatus(t, statuses())
	if got.State != tui.ProgramStatusIdle {
		t.Fatalf("reset status = %+v", got)
	}
}
