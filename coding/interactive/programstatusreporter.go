package interactive

import (
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// BlockedStatus is what a blocked program waits for.
type BlockedStatus struct {
	Kind    string
	Message string
}

func firstLine(text string) string {
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		text = text[:index]
	}
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		return trimmed
	}
	return "Error"
}

type programStatusBlocked struct {
	source string
	status BlockedStatus
}

// ProgramStatusReporter reports interactive-mode state to the terminal (OSC 7501): working
// during agent runs and compaction, blocked while a dialog waits for the user, then done,
// error or idle once the run settles. Messages are limited to the session name, dialog titles
// and the first line of errors; prompts and assistant output are never reported (upstream
// modes/interactive/program-status-reporter.ts).
type ProgramStatusReporter struct {
	getTerminal    func() tui.Terminal
	getAppName     func() string
	getSessionName func() string

	runActive  bool
	compacting bool
	// runResult is the outcome of the current run, reported once it settles.
	runResult tui.ProgramStatus
	// resting is the status while no run is active.
	resting tui.ProgramStatus
	// blocked are the open dialogs in the order they opened; the most recent one reports.
	blocked    []programStatusBlocked
	lastReport string
}

// NewProgramStatusReporter builds a reporter over the terminal, the app name and the session
// name.
func NewProgramStatusReporter(getTerminal func() tui.Terminal, getAppName func() string, getSessionName func() string) *ProgramStatusReporter {
	return &ProgramStatusReporter{
		getTerminal:    getTerminal,
		getAppName:     getAppName,
		getSessionName: getSessionName,
		runResult:      tui.ProgramStatus{State: tui.ProgramStatusDone},
		resting:        tui.ProgramStatus{State: tui.ProgramStatusIdle},
	}
}

// HandleEvent folds one session event into the reported state.
func (r *ProgramStatusReporter) HandleEvent(event *coding.SessionEvent) {
	if event == nil {
		return
	}
	switch event.Type {
	case coding.SessionAgentStart:
		r.runActive = true
		r.runResult = tui.ProgramStatus{State: tui.ProgramStatusDone}
	case coding.SessionMessageEnd:
		// The latest response decides the outcome, so a retried error is replaced by its
		// successful retry.
		assistant, ok := eventMessage(event).(*ai.AssistantMessage)
		if !ok {
			return
		}
		if assistant.StopReason == ai.StopError {
			r.runResult = tui.ProgramStatus{State: tui.ProgramStatusError, Message: firstLine(derefString(assistant.ErrorMessage))}
		} else {
			r.runResult = tui.ProgramStatus{State: tui.ProgramStatusDone}
		}
	case coding.SessionCompactionStart:
		r.compacting = true
	case coding.SessionCompactionEnd:
		r.compacting = false
		if r.runActive {
			// A failed recovery compaction ends the run unless a later response succeeds.
			if event.Aborted {
				r.runResult = tui.ProgramStatus{State: tui.ProgramStatusIdle}
			} else if event.ErrorMessage != "" {
				r.runResult = tui.ProgramStatus{State: tui.ProgramStatusError, Message: firstLine(event.ErrorMessage)}
			}
		} else if event.Aborted {
			r.resting = tui.ProgramStatus{State: tui.ProgramStatusIdle}
		} else if event.Reason == coding.CompactionManual {
			if event.ErrorMessage != "" {
				r.resting = tui.ProgramStatus{State: tui.ProgramStatusError, Message: firstLine(event.ErrorMessage)}
			} else {
				r.resting = tui.ProgramStatus{State: tui.ProgramStatusDone}
			}
		}
	case coding.SessionAgentSettled:
		r.runActive = false
		if event.Aborted {
			r.resting = tui.ProgramStatus{State: tui.ProgramStatusIdle}
		} else {
			r.resting = r.runResult
		}
	case coding.SessionInfoChanged:
		// The session name is part of the working and done reports.
	default:
		return
	}
	r.Report()
}

// SetBlocked reports a dialog as blocked until it is cleared with a nil status. Reopening a
// source replaces its status and makes it the most recent.
func (r *ProgramStatusReporter) SetBlocked(source string, status *BlockedStatus) {
	kept := make([]programStatusBlocked, 0, len(r.blocked)+1)
	for _, entry := range r.blocked {
		if entry.source != source {
			kept = append(kept, entry)
		}
	}
	if status != nil {
		kept = append(kept, programStatusBlocked{source: source, status: *status})
	}
	r.blocked = kept
	r.Report()
}

// Reset forgets the previous session's run, for example after switching sessions.
func (r *ProgramStatusReporter) Reset() {
	r.runActive = false
	r.compacting = false
	r.runResult = tui.ProgramStatus{State: tui.ProgramStatusDone}
	r.resting = tui.ProgramStatus{State: tui.ProgramStatusIdle}
	r.Report()
}

// Report sends the current status unless it is what was reported last.
func (r *ProgramStatusReporter) Report() {
	if r.getTerminal == nil {
		return
	}
	status := r.currentStatus()
	if r.getAppName != nil {
		status.App = r.getAppName()
	}
	key := status.State + "|" + status.Kind + "|" + status.Message + "|" + status.App
	if key == r.lastReport {
		return
	}
	r.lastReport = key
	if terminal := r.getTerminal(); terminal != nil {
		terminal.SetProgramStatus(status)
	}
}

func (r *ProgramStatusReporter) currentStatus() tui.ProgramStatus {
	if len(r.blocked) > 0 {
		blocked := r.blocked[len(r.blocked)-1].status
		return tui.ProgramStatus{State: tui.ProgramStatusBlocked, Kind: blocked.Kind, Message: blocked.Message}
	}
	if r.compacting {
		return tui.ProgramStatus{State: tui.ProgramStatusWorking, Message: "Compacting context"}
	}
	status := r.resting
	if r.runActive {
		status = tui.ProgramStatus{State: tui.ProgramStatusWorking}
	}
	if status.State == tui.ProgramStatusWorking || status.State == tui.ProgramStatusDone {
		if r.getSessionName != nil {
			status.Message = r.getSessionName()
		}
	}
	return status
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
