package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
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

	// Tokens: the streaming message's usage so far, then each finished
	// message's, summed over the run. In counts input, cache reads and writes.
	message := func(eventType string, usage ai.Usage) *coding.SessionEvent {
		kind := map[string]string{coding.SessionMessageStart: "message_start", coding.SessionMessageUpdate: "message_update", coding.SessionMessageEnd: "message_end"}[eventType]
		return &coding.SessionEvent{Type: eventType, Agent: agentEvent(kind, &ai.AssistantMessage{Usage: usage, StopReason: ai.StopStop})}
	}
	dispatcher.HandleEvent(message(coding.SessionMessageStart, ai.Usage{}))
	dispatcher.HandleEvent(message(coding.SessionMessageUpdate, ai.Usage{Input: 1200, CacheRead: 30000, Output: 50}))
	if got := renderedText(working.Render(80)); !strings.Contains(got, "Working (1m 15s · ↑31k ↓50)") {
		t.Fatalf("streaming tokens = %q", got)
	}
	dispatcher.HandleEvent(message(coding.SessionMessageEnd, ai.Usage{Input: 1200, CacheRead: 30000, CacheWrite: 800, Output: 400}))
	dispatcher.HandleEvent(message(coding.SessionMessageStart, ai.Usage{}))
	dispatcher.HandleEvent(message(coding.SessionMessageUpdate, ai.Usage{Input: 10, CacheRead: 32000, Output: 2800}))
	if got := renderedText(working.Render(80)); !strings.Contains(got, "Working (1m 15s · ↑64k ↓3.2k)") {
		t.Fatalf("second message tokens = %q", got)
	}

	// A chunk that omits a field does not lower the message's count.
	dispatcher.HandleEvent(message(coding.SessionMessageUpdate, ai.Usage{Input: 10, CacheRead: 32000}))
	if got := renderedText(working.Render(80)); !strings.Contains(got, "↑64k ↓3.2k") {
		t.Fatalf("a lower partial usage changed the count: %q", got)
	}
	dispatcher.HandleEvent(message(coding.SessionMessageEnd, ai.Usage{Input: 10, CacheRead: 32000, Output: 2800}))

	// A retry ends the agent run and starts another inside the same working
	// period: no notice yet, and the clock and tokens keep counting.
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd, WillRetry: true})
	if got := renderedText(transcript.Chat.Render(80)); strings.Contains(got, "Worked for") {
		t.Fatalf("agent_end before settling added a notice: %q", got)
	}
	workingClock = func() time.Time { return started.Add(80 * time.Second) }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionTurnStart})
	working = uiState.ActiveStatusIndicator.(*StatusIndicator)
	if got := renderedText(working.Render(80)); !strings.Contains(got, "Working (1m 15s · ↑64k ↓3.2k)") {
		t.Fatalf("after the retry's agent_start = %q, want the period's running totals", got)
	}
	dispatcher.HandleEvent(message(coding.SessionMessageStart, ai.Usage{}))
	dispatcher.HandleEvent(message(coding.SessionMessageEnd, ai.Usage{Input: 6000, Output: 1000}))
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})

	workingClock = func() time.Time { return started.Add(83*time.Second + 400*time.Millisecond) }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})
	if got := renderedText(transcript.Chat.Render(80)); !strings.Contains(got, "Worked for 1m 23s · ↑70k ↓4.2k") {
		t.Fatalf("transcript = %q, want the worked-for notice for the whole period", got)
	}
	if strings.Count(renderedText(transcript.Chat.Render(80)), "Worked for") != 1 {
		t.Fatal("the working period added more than one notice")
	}
	// The notice is its own line: a later status does not replace it.
	transcript.ShowStatus("Model switched")
	if got := renderedText(transcript.Chat.Render(80)); !strings.Contains(got, "Worked for 1m 23s") || !strings.Contains(got, "Model switched") {
		t.Fatalf("transcript after a status = %q", got)
	}
	// Settling without a running clock (a replayed event) adds nothing.
	before := len(transcript.Chat.Children)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})
	if len(transcript.Chat.Children) != before {
		t.Fatal("an agent end without a running clock added a notice")
	}
}

func TestWorkingElapsedLabelFormats(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		tokens  workingTokenCount
		want    string
	}{
		{0, workingTokenCount{}, "(0s)"},
		{999 * time.Millisecond, workingTokenCount{}, "(0s)"},
		{59 * time.Second, workingTokenCount{in: 950, out: 0}, "(59s · ↑950 ↓0)"},
		{65 * time.Second, workingTokenCount{in: 48200, out: 3240}, "(1m 5s · ↑48k ↓3.2k)"},
		{3723 * time.Second, workingTokenCount{in: 2_400_000, out: 51_000}, "(1h 2m 3s · ↑2.4M ↓51k)"},
	} {
		if got := workingElapsedLabel(tc.elapsed, tc.tokens); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.elapsed, got, tc.want)
		}
	}
}

// D402: pier addition. A goal keeps the working period open across its
// rounds (pi v1.5.0 has no working clock; goal rounds follow agent_settled in
// coding-agent/src/core/goal), so the notice comes once the goal stops being
// pursued, and a prompt after an idle goal closes the period at its last
// settle.
func TestWorkingPeriodSpansGoalRounds(t *testing.T) {
	dispatcher, _, transcript, uiState := newEventTestDispatcher(t)
	started := time.Now()
	at := func(d time.Duration) { workingClock = func() time.Time { return started.Add(d) } }
	saved := workingClock
	t.Cleanup(func() { workingClock = saved })
	assistant := func(eventType string, usage ai.Usage) *coding.SessionEvent {
		kind := map[string]string{coding.SessionMessageStart: "message_start", coding.SessionMessageEnd: "message_end"}[eventType]
		return &coding.SessionEvent{Type: eventType, Agent: agentEvent(kind, &ai.AssistantMessage{Usage: usage, StopReason: ai.StopStop})}
	}
	round := func(usage ai.Usage, settledAt time.Duration, pursuing bool) {
		dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
		dispatcher.HandleEvent(assistant(coding.SessionMessageStart, ai.Usage{}))
		dispatcher.HandleEvent(assistant(coding.SessionMessageEnd, usage))
		dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
		at(settledAt)
		dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled, GoalPursuing: pursuing})
	}
	notices := func() string { return renderedText(transcript.Chat.Render(80)) }

	at(0)
	round(ai.Usage{Input: 1000, Output: 100}, 10*time.Second, true)
	if strings.Contains(notices(), "Worked for") {
		t.Fatalf("a goal round still pursuing added a notice: %q", notices())
	}
	at(11 * time.Second)
	round(ai.Usage{Input: 2000, Output: 200}, 20*time.Second, false)
	if got := notices(); strings.Count(got, "Worked for") != 1 || !strings.Contains(got, "Worked for 20.0s · ↑3.0k ↓300") {
		t.Fatalf("transcript = %q, want one notice for both goal rounds", got)
	}

	// An aborted settle ends the period even while the goal is active.
	at(30 * time.Second)
	round(ai.Usage{Output: 5}, 32*time.Second, true)
	before := strings.Count(notices(), "Worked for")
	at(33 * time.Second)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	at(34 * time.Second)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled, Aborted: true, GoalPursuing: true})
	if got := strings.Count(notices(), "Worked for"); got != before+1 || !strings.Contains(notices(), "Worked for 4.0s") {
		t.Fatalf("aborted goal round: %q", notices())
	}

	// No round follows a pursuing settle: the next prompt closes the period
	// at that settle, then times its own.
	at(40 * time.Second)
	round(ai.Usage{Output: 7}, 45*time.Second, true)
	at(90 * time.Second)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "next"}})})
	if !strings.Contains(notices(), "Worked for 5.0s · ↑0 ↓7") {
		t.Fatalf("idle goal period: %q", notices())
	}
	if !uiState.WorkingClockRunning() {
		t.Fatal("the prompt after an idle goal did not start a new period")
	}
}

// D402/D385: a goal that held Alt+Enter follow-ups releases them as another run
// after its own settles. That released run belongs to the same working period,
// so the follow-up's time and tokens are added to the agent's instead of
// starting a second period with its own notice.
func TestWorkingPeriodSpansTheReleasedFollowUpRun(t *testing.T) {
	dispatcher, _, transcript, uiState := newEventTestDispatcher(t)
	started := time.Now()
	at := func(d time.Duration) { workingClock = func() time.Time { return started.Add(d) } }
	saved := workingClock
	t.Cleanup(func() { workingClock = saved })
	assistant := func(eventType string, usage ai.Usage) *coding.SessionEvent {
		kind := map[string]string{coding.SessionMessageStart: "message_start", coding.SessionMessageEnd: "message_end"}[eventType]
		return &coding.SessionEvent{Type: eventType, Agent: agentEvent(kind, &ai.AssistantMessage{Usage: usage, StopReason: ai.StopStop})}
	}
	notices := func() string { return renderedText(transcript.Chat.Render(80)) }

	// The goal's chain: two goal rounds inside one run, then a settle that
	// reports the released follow-up run still to come.
	at(0)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	dispatcher.HandleEvent(assistant(coding.SessionMessageStart, ai.Usage{}))
	dispatcher.HandleEvent(assistant(coding.SessionMessageEnd, ai.Usage{Input: 1000, Output: 100}))
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
	at(20 * time.Second)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled, FollowUpsPending: true})
	if got := notices(); strings.Contains(got, "Worked for") {
		t.Fatalf("settling with a released follow-up run pending added a notice: %q", got)
	}
	if !uiState.WorkingClockRunning() {
		t.Fatal("the period closed before the released follow-up ran")
	}

	// The released run: the clock and tokens continue, no second period. Its
	// transcript opens with the held follow-up's user message, which belongs to
	// this period just as a goal round's custom message does, so it must not be
	// mistaken for a fresh prompt that closes the waiting period.
	at(21 * time.Second)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "after the goal"}})})
	dispatcher.HandleEvent(assistant(coding.SessionMessageStart, ai.Usage{}))
	dispatcher.HandleEvent(assistant(coding.SessionMessageEnd, ai.Usage{Input: 500, Output: 50}))
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
	at(25 * time.Second)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})
	if got := notices(); strings.Count(got, "Worked for") != 1 || !strings.Contains(got, "Worked for 25.0s · ↑1.5k ↓150") {
		t.Fatalf("transcript = %q, want one notice covering the goal and its follow-up", got)
	}
}
