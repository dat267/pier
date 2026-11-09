package interactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D402: a working period belongs to the session it timed. Replacing the session
// (D-rows: /new, /import, /resume, /fork) discards the period instead of
// carrying it over, so the new session's first notice covers only its own run
// and the elapsed time it shows is not the old session's.
//
// Upstream resets this state per command (interactive-mode.ts handleResumeSession,
// handleImportCommand, handleClearCommand, stop all begin with
// clearStatusIndicator); the clock itself is pier's addition, so the port clears
// it at the one chokepoint every replacement passes: teardownCurrentSession.
func TestSessionSwitchDiscardsTheOldWorkingPeriod(t *testing.T) {
	for _, tc := range []struct {
		name      string
		goalRound bool
	}{
		// The session was streaming when it was replaced.
		{name: "running"},
		// The session's goal round settled and waits for the next round.
		{name: "waiting for a goal round", goalRound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			target := makePersistedSession(t, makeTrustRequiringProject(t, "alpha-theme"), "other session")
			started := time.Now().Add(-90 * time.Second)
			clock := started
			saved := workingClock
			workingClock = func() time.Time { return clock }
			t.Cleanup(func() { workingClock = saved })

			assistant := func(usage ai.Usage) []*coding.SessionEvent {
				return []*coding.SessionEvent{
					{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", &ai.AssistantMessage{Usage: ai.Usage{}})},
					{Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", &ai.AssistantMessage{Usage: usage, StopReason: ai.StopStop})},
				}
			}
			// The replaced session's period: a round with tokens of its own.
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionTurnStart})
			for _, event := range assistant(ai.Usage{Input: 4000, Output: 40}) {
				app.events.HandleEvent(event)
			}
			if tc.goalRound {
				app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
				app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled, GoalPursuing: true})
			}
			if !app.uiState.WorkingClockRunning() {
				t.Fatal("the replaced session was not being timed")
			}

			if _, err := app.switchSession(context.Background(), target.GetSessionFile(), ""); err != nil {
				t.Fatalf("switch: %v", err)
			}
			if app.uiState.WorkingClockRunning() {
				t.Fatal("the replaced session's working period survived the switch")
			}
			// The spinner belonged to that period too: it must not keep ticking
			// over the new session, where nothing is running yet.
			if app.uiState.ActiveStatusIndicator != nil {
				t.Fatalf("the replaced session's %q indicator survived the switch",
					app.uiState.ActiveStatusIndicator.IndicatorKind())
			}

			// The new session's own run: five seconds, its own tokens.
			clock = time.Now()
			newStart := clock
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "fresh prompt"}})})
			for _, event := range assistant(ai.Usage{Input: 10, Output: 2}) {
				app.events.HandleEvent(event)
			}
			clock = newStart.Add(5 * time.Second)
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})

			notices := strings.Join(app.chat.Render(120), "\n")
			if got := strings.Count(notices, "Worked for"); got != 1 {
				t.Fatalf("notices = %d, want the new session's one:\n%s", got, notices)
			}
			if !strings.Contains(notices, "Worked for 5.0s · ↑10 ↓2") {
				t.Fatalf("transcript = %q, want only the new session's work", notices)
			}
		})
	}
}
