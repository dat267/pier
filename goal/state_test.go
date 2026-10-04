package goal

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Mirrors goal/state.test.ts.

const testT0 int64 = 1_000

func testNow(ms int64) time.Time { return time.UnixMilli(ms) }

func changeEntry(t *testing.T, operation GoalOperation, goal *GoalSnapshot, timestamp int64, cleared *ClearedRef) CustomEntry {
	t.Helper()
	data, err := json.Marshal(GoalChangeEntry{Operation: operation, Goal: goal, Cleared: cleared, Timestamp: timestamp})
	if err != nil {
		t.Fatal(err)
	}
	return CustomEntry{CustomType: GoalCustomType, Data: data}
}

func turnEntry(t *testing.T, goalID string, revision, turn int, timestamp int64) CustomEntry {
	t.Helper()
	data, err := json.Marshal(GoalTurnEntry{GoalID: goalID, Revision: revision, Turn: turn, Timestamp: timestamp})
	if err != nil {
		t.Fatal(err)
	}
	return CustomEntry{CustomType: GoalTurnType, Data: data}
}

func settingsEntry(t *testing.T, bannerEnabled bool, timestamp int64) CustomEntry {
	t.Helper()
	data, err := json.Marshal(GoalSettingsEntry{BannerEnabled: bannerEnabled, Timestamp: timestamp})
	if err != nil {
		t.Fatal(err)
	}
	return CustomEntry{CustomType: GoalSettingsType, Data: data}
}

func mustFoldError(t *testing.T, entries []CustomEntry, want string) {
	t.Helper()
	_, err := FoldGoal(entries)
	if err == nil {
		t.Fatalf("expected an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestCreateGoalStateProducesRevision1ActiveGoal(t *testing.T) {
	goal := CreateGoalState("do the thing", testNow(testT0))
	if goal.Revision != 1 {
		t.Fatalf("revision = %d", goal.Revision)
	}
	if goal.Phase != PhaseActive {
		t.Fatalf("phase = %s", goal.Phase)
	}
	if !strings.HasPrefix(goal.ID, "goal-") {
		t.Fatalf("id = %s", goal.ID)
	}
}

func TestFoldReplaysLifecycleChangesAndTurnEntries(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	paused := goal
	paused.Phase = PhasePaused
	paused.BlockedReason = &BlockedReason{Code: "human-paused", Message: "m"}
	paused.Revision = 2
	paused.UpdatedAt = testT0 + 100
	resumed := goal
	resumed.Phase = PhaseActive
	resumed.Revision = 3
	resumed.UpdatedAt = testT0 + 200

	folded, err := FoldGoal([]CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		turnEntry(t, goal.ID, 1, 1, testT0+10),
		changeEntry(t, OpPause, &paused, testT0+100, nil),
		changeEntry(t, OpResume, &resumed, testT0+200, nil),
		turnEntry(t, goal.ID, 3, 2, testT0+300),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal == nil || folded.Goal.Phase != PhaseActive || folded.Goal.Revision != 3 || folded.Goal.TurnsStarted != 2 || folded.Goal.Armed {
		t.Fatalf("goal = %+v", folded.Goal)
	}
}

func TestFoldReturnsNilAfterAClearTombstone(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpClear, nil, testT0+100, &ClearedRef{ID: goal.ID, Revision: goal.Revision}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal != nil {
		t.Fatalf("goal = %+v", folded.Goal)
	}
}

func TestFoldRejectsAStaleClear(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	mustFoldError(t, []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpClear, nil, testT0+100, &ClearedRef{ID: goal.ID, Revision: 99}),
	}, "stale clear")
}

func TestFoldRejectsDiscontinuousRevisions(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	skip := goal
	skip.Phase = PhasePaused
	skip.BlockedReason = &BlockedReason{Code: "x", Message: "m"}
	skip.Revision = 5
	skip.UpdatedAt = testT0 + 100
	mustFoldError(t, []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpPause, &skip, testT0+100, nil),
	}, "discontinuous")
}

func TestFoldRejectsIllegalTransitions(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	paused := goal
	paused.Phase = PhasePaused
	paused.BlockedReason = &BlockedReason{Code: "x", Message: "m"}
	paused.Revision = 2
	paused.UpdatedAt = testT0 + 100
	pausedAgain := paused
	pausedAgain.Revision = 3
	pausedAgain.UpdatedAt = testT0 + 200
	mustFoldError(t, []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpPause, &paused, testT0+100, nil),
		changeEntry(t, OpPause, &pausedAgain, testT0+200, nil),
	}, "illegal transition")

	resumeToComplete := goal
	resumeToComplete.Phase = PhaseComplete
	resumeToComplete.Revision = 2
	resumeToComplete.UpdatedAt = testT0 + 100
	mustFoldError(t, []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpResume, &resumeToComplete, testT0+100, nil),
	}, "must produce phase active")
}

func TestFoldRejectsNonSequentialGoalTurns(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	mustFoldError(t, []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		turnEntry(t, goal.ID, 1, 2, testT0+10),
	}, "non-sequential")
}

func TestFoldIgnoresTurnEntriesFromAPreviousGoal(t *testing.T) {
	g1 := CreateGoalState("first", testNow(testT0))
	g2 := CreateGoalState("second", testNow(testT0+500))
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(t, OpCreate, &g1, testT0, nil),
		turnEntry(t, g1.ID, 1, 1, testT0+10),
		changeEntry(t, OpClear, nil, testT0+100, &ClearedRef{ID: g1.ID, Revision: 1}),
		changeEntry(t, OpCreate, &g2, testT0+500, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal == nil || folded.Goal.ID != g2.ID || folded.Goal.TurnsStarted != 0 {
		t.Fatalf("goal = %+v", folded.Goal)
	}
}

func TestFoldRejectsTimestampRegression(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	older := goal
	older.Phase = PhasePaused
	older.BlockedReason = &BlockedReason{Code: "x", Message: "m"}
	older.Revision = 2
	older.UpdatedAt = testT0 - 1
	mustFoldError(t, []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpPause, &older, testT0-1, nil),
	}, "regression")
}

func TestApplyChangeEnforcesCASRevision(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	bad := goal
	bad.Revision = 7
	if _, err := ApplyChange(&goal, GoalChangeEntry{Operation: OpPause, Goal: &bad, Timestamp: testT0 + 1}); err == nil || !strings.Contains(err.Error(), "discontinuous") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnteringBlockedRequiresABlockerReason(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	noReason := goal
	noReason.Phase = PhaseBlocked
	noReason.Revision = 2
	noReason.UpdatedAt = testT0 + 1
	if _, err := ApplyChange(&goal, GoalChangeEntry{Operation: OpBlock, Goal: &noReason, Timestamp: testT0 + 1}); err == nil || !strings.Contains(err.Error(), "blocker reason") {
		t.Fatalf("err = %v", err)
	}
	withReason := noReason
	withReason.BlockedReason = &BlockedReason{Code: "x", Message: "m"}
	if _, err := ApplyChange(&goal, GoalChangeEntry{Operation: OpBlock, Goal: &withReason, Timestamp: testT0 + 1}); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func viewWith(g GoalSnapshot, armed bool, turnsStarted int) *GoalView {
	return &GoalView{GoalSnapshot: g, Armed: armed, TurnsStarted: turnsStarted}
}

func TestStatusLine(t *testing.T) {
	line := StatusLine(viewWith(CreateGoalState("obj", testNow(testT0)), true, 2))
	if line != "active ▶ 2 rounds" {
		t.Fatalf("line = %q", line)
	}
	if StatusLine(nil) != "" {
		t.Fatalf("nil status = %q", StatusLine(nil))
	}
}

func TestGoalStatusMessage(t *testing.T) {
	message := GoalStatusMessage(viewWith(CreateGoalState("obj", testNow(testT0)), true, 2), true)
	if message != "active ▶ 2 rounds\nobj\nBanner: on (bare /goal to toggle)" {
		t.Fatalf("message = %q", message)
	}
	if got := GoalStatusMessage(nil, false); got != "No goal set. Use /goal set <objective>\nBanner: off (bare /goal to toggle)" {
		t.Fatalf("nil message = %q", got)
	}
}

func TestTruncateObjective(t *testing.T) {
	if got := TruncateObjective("  a\n\nb  ", 60); got != "a b" {
		t.Fatalf("got = %q", got)
	}
	if got := TruncateObjective(strings.Repeat("x", 100), 10); got != strings.Repeat("x", 9)+"…" {
		t.Fatalf("got = %q", got)
	}
}

func TestGoalViewShapesTheContract(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	result := GoalViewOf(viewWith(goal, true, 2))
	if result.Goal == nil || result.Goal.ID != goal.ID || result.Goal.Revision != 1 || result.Goal.Objective != "obj" || result.Goal.Phase != PhaseActive || result.Goal.TurnsStarted != 2 {
		t.Fatalf("goal = %+v", result.Goal)
	}
	if result.Activation != "armed" {
		t.Fatalf("activation = %q", result.Activation)
	}
}

func TestGoalViewOmitsBlockedReasonAndReportsNull(t *testing.T) {
	blocked := CreateGoalState("obj", testNow(testT0))
	blocked.Phase = PhaseBlocked
	blocked.BlockedReason = &BlockedReason{Code: "stuck", Message: "no path"}
	result := GoalViewOf(viewWith(blocked, false, 0))
	if result.Goal == nil || result.Goal.BlockedReason == nil || result.Goal.BlockedReason.Message != "no path" {
		t.Fatalf("blocked = %+v", result.Goal)
	}
	if result.Activation != "disarmed" {
		t.Fatalf("activation = %q", result.Activation)
	}

	clean := GoalViewOf(viewWith(CreateGoalState("obj", testNow(testT0)), false, 0))
	encoded, _ := json.Marshal(clean.Goal)
	if strings.Contains(string(encoded), "blockedReason") {
		t.Fatalf("clean goal leaked blockedReason: %s", encoded)
	}
	nullResult := GoalViewOf(nil)
	if nullResult.Goal != nil {
		t.Fatalf("null goal = %+v", nullResult.Goal)
	}
}

func TestResumeHint(t *testing.T) {
	cases := map[string]string{
		"api-auth":     "API key",
		"api-billing":  "credits",
		"api-request":  "request",
		"api-error":    "recovers",
		"human-paused": "/goal resume",
	}
	for code, want := range cases {
		if got := ResumeHint(BlockedReason{Code: code, Message: "x"}); !strings.Contains(got, want) {
			t.Fatalf("code %s: got %q, want %q", code, got, want)
		}
	}
}

func TestFoldReportsBannerEnabledFromTheLastSettingsEntry(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	if folded, _ := FoldGoal(nil); folded.BannerEnabled {
		t.Fatal("empty fold must default the banner off")
	}
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		settingsEntry(t, true, testT0+1),
		settingsEntry(t, false, testT0+2),
		settingsEntry(t, true, testT0+3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !folded.BannerEnabled || folded.Goal == nil || folded.Goal.ID != goal.ID {
		t.Fatalf("folded = %+v", folded)
	}
}

func TestFoldKeepsTheBannerFlagAfterClear(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		settingsEntry(t, true, testT0+1),
		changeEntry(t, OpClear, nil, testT0+2, &ClearedRef{ID: goal.ID, Revision: goal.Revision}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal != nil || !folded.BannerEnabled {
		t.Fatalf("folded = %+v", folded)
	}
}

func TestCreateGoalStateStampsTheStateVersion(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	if goal.Version == nil || *goal.Version != 1 {
		t.Fatalf("version = %v", goal.Version)
	}
}

func TestApplyChangeRejectsAnUnknownStateVersion(t *testing.T) {
	goal := CreateGoalState("obj", testNow(testT0))
	version := 2
	goal.Version = &version
	if _, err := ApplyChange(nil, GoalChangeEntry{Operation: OpCreate, Goal: &goal, Timestamp: testT0}); err == nil || !strings.Contains(err.Error(), "unsupported goal state version") {
		t.Fatalf("err = %v", err)
	}
	legacy := CreateGoalState("obj", testNow(testT0))
	legacy.Version = nil
	if _, err := ApplyChange(nil, GoalChangeEntry{Operation: OpCreate, Goal: &legacy, Timestamp: testT0}); err != nil {
		t.Fatalf("legacy err = %v", err)
	}
}

func TestGoalRoundPromptFramesTheObjectiveAsUntrusted(t *testing.T) {
	goal := CreateGoalState("delete every table in prod", testNow(testT0))
	prompt := GoalRoundPrompt(GoalView{GoalSnapshot: goal, Armed: true, TurnsStarted: 2}, 3)
	for _, want := range []string{
		"<untrusted_objective>delete every table in prod</untrusted_objective>",
		"user-provided data",
		"Round 3.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
