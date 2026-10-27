package goal

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Mirrors ~/.pi/agent/extensions/goal/state.test.ts.

const t0 int64 = 1_000

func testTime(ms int64) time.Time { return time.UnixMilli(ms) }

func changeEntry(operation GoalOperation, goal *GoalSnapshot, timestamp int64, cleared *ClearedRef) CustomEntry {
	var entry GoalChangeEntry
	if cleared != nil {
		entry = GoalChangeEntry{Operation: operation, Cleared: cleared, Timestamp: timestamp}
	} else {
		entry = GoalChangeEntry{Operation: operation, Goal: goal, Timestamp: timestamp}
	}
	data, _ := json.Marshal(entry)
	return CustomEntry{CustomType: GoalCustomType, Data: data}
}

func turnEntry(goalID string, revision, turn int, timestamp int64, usage *GoalUsage) CustomEntry {
	data, _ := json.Marshal(GoalTurnEntry{GoalID: goalID, Revision: revision, Turn: turn, Timestamp: timestamp, Usage: usage})
	return CustomEntry{CustomType: GoalTurnType, Data: data}
}

func createTestGoal(objective string, now int64, budget *GoalBudget) GoalSnapshot {
	return CreateGoalState(objective, testTime(now), budget)
}

func TestCreateProducesRevisionOneActiveGoal(t *testing.T) {
	g := createTestGoal("do the thing", t0, nil)
	if g.Revision != 1 || g.Phase != PhaseActive {
		t.Fatalf("revision=%d phase=%s", g.Revision, g.Phase)
	}
	if !regexp.MustCompile(`^goal-`).MatchString(g.ID) {
		t.Fatalf("id %q", g.ID)
	}
}

func TestFoldReplaysLifecycleAndTurns(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	paused := g
	paused.Phase, paused.Revision, paused.UpdatedAt = PhasePaused, 2, t0+100
	paused.BlockedReason = &BlockedReason{Code: "human-paused", Message: "m"}
	resumed := g
	resumed.Phase, resumed.Revision, resumed.UpdatedAt = PhaseActive, 3, t0+200
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		turnEntry(g.ID, 1, 1, t0+10, nil),
		changeEntry(OpPause, &paused, t0+100, nil),
		changeEntry(OpResume, &resumed, t0+200, nil),
		turnEntry(g.ID, 3, 2, t0+300, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal.Phase != PhaseActive || folded.Goal.Revision != 3 || folded.Goal.TurnsStarted != 2 || folded.Goal.Armed {
		t.Fatalf("unexpected view: %+v", folded.Goal)
	}
}

func TestFoldRestoresCumulativeUsage(t *testing.T) {
	budget := &GoalBudget{TimeLimitMs: new(int64(1000)), TokenLimit: new(int64(500)), CostLimitUsd: new(float64(2))}
	g := createTestGoal("obj", t0, budget)
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		turnEntry(g.ID, 1, 1, t0+1, &GoalUsage{Tokens: 120, CostUsd: 0.25}),
		turnEntry(g.ID, 1, 2, t0+2, &GoalUsage{Tokens: 80, CostUsd: 0.15}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal.UsedTokens != 200 || folded.Goal.UsedCostUsd != 0.4 {
		t.Fatalf("usage tokens=%d cost=%v", folded.Goal.UsedTokens, folded.Goal.UsedCostUsd)
	}
	if folded.Goal.Budget == nil || *folded.Goal.Budget.TokenLimit != 500 {
		t.Fatalf("budget %+v", folded.Goal.Budget)
	}
}

func TestFoldResetsUsageWhenANewGoalReplacesACompletedOne(t *testing.T) {
	first := createTestGoal("first", t0, &GoalBudget{TokenLimit: new(int64(500))})
	done := first
	done.Phase, done.Revision, done.UpdatedAt = PhaseComplete, 2, t0+2
	next := createTestGoal("second", t0+3, nil)
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &first, t0, nil),
		turnEntry(first.ID, 1, 1, t0+1, &GoalUsage{Tokens: 250, CostUsd: 1}),
		changeEntry(OpComplete, &done, t0+2, nil),
		changeEntry(OpCreate, &next, t0+3, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal.ID != next.ID || folded.Goal.TurnsStarted != 0 || folded.Goal.UsedTokens != 0 || folded.Goal.UsedCostUsd != 0 {
		t.Fatalf("unexpected fresh view: %+v", folded.Goal)
	}
}

func TestFoldReturnsNilAfterAClearTombstone(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		changeEntry(OpClear, nil, t0+100, &ClearedRef{ID: g.ID, Revision: g.Revision}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal != nil {
		t.Fatalf("expected nil goal, got %+v", folded.Goal)
	}
}

func TestClearRejectsAStaleRevision(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	_, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		changeEntry(OpClear, nil, t0+100, &ClearedRef{ID: g.ID, Revision: 99}),
	})
	if err == nil || !strings.Contains(err.Error(), "stale clear") {
		t.Fatalf("err=%v", err)
	}
}

func TestFoldRejectsDiscontinuousRevisions(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	skip := g
	skip.Phase, skip.BlockedReason, skip.Revision, skip.UpdatedAt = PhasePaused, &BlockedReason{Code: "x", Message: "m"}, 5, t0+100
	_, err := FoldGoal([]CustomEntry{changeEntry(OpCreate, &g, t0, nil), changeEntry(OpPause, &skip, t0+100, nil)})
	if err == nil || !strings.Contains(err.Error(), "discontinuous") {
		t.Fatalf("err=%v", err)
	}
}

func TestFoldRejectsIllegalPhaseTransitions(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	paused := g
	paused.Phase, paused.BlockedReason, paused.Revision, paused.UpdatedAt = PhasePaused, &BlockedReason{Code: "x", Message: "m"}, 2, t0+100
	pausedAgain := paused
	pausedAgain.Revision, pausedAgain.UpdatedAt = 3, t0+200
	_, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		changeEntry(OpPause, &paused, t0+100, nil),
		changeEntry(OpPause, &pausedAgain, t0+200, nil),
	})
	if err == nil || !strings.Contains(err.Error(), "illegal transition") {
		t.Fatalf("err=%v", err)
	}
	// An operation must agree with the phase it produces.
	complete := g
	complete.Phase, complete.Revision, complete.UpdatedAt = PhaseComplete, 2, t0+100
	_, err = FoldGoal([]CustomEntry{changeEntry(OpCreate, &g, t0, nil), changeEntry(OpResume, &complete, t0+100, nil)})
	if err == nil || !strings.Contains(err.Error(), "must produce phase active") {
		t.Fatalf("err=%v", err)
	}
}

func TestFoldRejectsNonSequentialTurns(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	_, err := FoldGoal([]CustomEntry{changeEntry(OpCreate, &g, t0, nil), turnEntry(g.ID, 1, 2, t0+10, nil)})
	if err == nil || !strings.Contains(err.Error(), "non-sequential") {
		t.Fatalf("err=%v", err)
	}
}

func TestFoldIgnoresTurnsFromAPreviousGoal(t *testing.T) {
	g1 := createTestGoal("first", t0, nil)
	g2 := createTestGoal("second", t0+500, nil)
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g1, t0, nil),
		turnEntry(g1.ID, 1, 1, t0+10, nil),
		changeEntry(OpClear, nil, t0+100, &ClearedRef{ID: g1.ID, Revision: 1}),
		changeEntry(OpCreate, &g2, t0+500, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal.ID != g2.ID || folded.Goal.TurnsStarted != 0 {
		t.Fatalf("unexpected view: %+v", folded.Goal)
	}
}

func TestFoldRejectsTimestampRegression(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	older := g
	older.Phase, older.BlockedReason, older.Revision, older.UpdatedAt = PhasePaused, &BlockedReason{Code: "x", Message: "m"}, 2, t0-1
	_, err := FoldGoal([]CustomEntry{changeEntry(OpCreate, &g, t0, nil), changeEntry(OpPause, &older, t0-1, nil)})
	if err == nil || !strings.Contains(err.Error(), "regression") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyChangeEnforcesCASRevision(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	wrong := g
	wrong.Revision = 7
	_, err := ApplyChange(&g, GoalChangeEntry{Operation: OpPause, Goal: &wrong, Timestamp: t0 + 1})
	if err == nil || !strings.Contains(err.Error(), "discontinuous") {
		t.Fatalf("err=%v", err)
	}
}

func TestEnteringBlockedRequiresABlockerReason(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	noReason := g
	noReason.Phase, noReason.Revision, noReason.UpdatedAt = PhaseBlocked, 2, t0+1
	_, err := ApplyChange(&g, GoalChangeEntry{Operation: OpBlock, Goal: &noReason, Timestamp: t0 + 1})
	if err == nil || !strings.Contains(err.Error(), "blocker reason") {
		t.Fatalf("err=%v", err)
	}
	withReason := noReason
	withReason.BlockedReason = &BlockedReason{Code: "x", Message: "m"}
	if _, err := ApplyChange(&g, GoalChangeEntry{Operation: OpBlock, Goal: &withReason, Timestamp: t0 + 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusLineShowsPhaseArmMarkerAndRoundCount(t *testing.T) {
	g := GoalView{GoalSnapshot: createTestGoal("obj", t0, nil), Armed: true, TurnsStarted: 2}
	if got := StatusLine(&g); got != "active ▶ 2 rounds" {
		t.Fatalf("got %q", got)
	}
	if StatusLine(nil) != "" {
		t.Fatal("nil goal must render an empty status line")
	}
}

func TestGoalStatusMessageReportsStateWithoutBannerInstructions(t *testing.T) {
	g := GoalView{GoalSnapshot: createTestGoal("obj", t0, nil), Armed: true, TurnsStarted: 2}
	if got := GoalStatusMessage(&g, testTime(t0)); got != "active ▶ 2 rounds\nobj" {
		t.Fatalf("got %q", got)
	}
	if got := GoalStatusMessage(nil, testTime(t0)); got != "No goal set. Use /goal set <objective>" {
		t.Fatalf("got %q", got)
	}
}

func TestGoalStatusPreservesTheFullObjective(t *testing.T) {
	objective := "Complete the entire long-running project objective without shortening its final acceptance criteria or verification instructions."
	g := GoalView{GoalSnapshot: createTestGoal(objective, t0, nil)}
	if got := GoalStatusMessage(&g, testTime(t0)); got != "active 0 rounds\n"+objective {
		t.Fatalf("got %q", got)
	}
}

func TestGoalStatusReportsLimitsAndSpentUsage(t *testing.T) {
	snapshot := createTestGoal("obj", t0, &GoalBudget{TimeLimitMs: new(int64(8 * 3_600_000)), TokenLimit: new(int64(500_000)), CostLimitUsd: new(float64(10))})
	g := GoalView{GoalSnapshot: snapshot, TurnsStarted: 2, UsedTokens: 100_000, UsedCostUsd: 2.5}
	message := GoalStatusMessage(&g, testTime(t0))
	if !regexp.MustCompile(`Budget: time \d+/480m, tokens 100000/500000, reported cost \$2\.50/\$10\.00`).MatchString(message) {
		t.Fatalf("got %q", message)
	}
}

func TestTruncateObjectiveFlattensWhitespaceAndCapsLength(t *testing.T) {
	if got := TruncateObjective("  a\n\nb  ", 60); got != "a b" {
		t.Fatalf("got %q", got)
	}
	if got := TruncateObjective(strings.Repeat("x", 100), 10); got != strings.Repeat("x", 9)+"…" {
		t.Fatalf("got %q", got)
	}
}

func TestGoalViewShapesTheGetGoalContract(t *testing.T) {
	g := GoalView{GoalSnapshot: createTestGoal("obj", t0, nil), Armed: true, TurnsStarted: 2}
	result := GoalViewOf(&g)
	if result.Activation != "armed" || result.Goal == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	data, _ := json.Marshal(result)
	expected := `{"goal":{"id":"` + g.ID + `","revision":1,"objective":"obj","phase":"active","turnsStarted":2,"usedTokens":0,"usedCostUsd":0},"activation":"armed"}`
	if string(data) != expected {
		t.Fatalf("got %s\nwant %s", data, expected)
	}
}

func TestGoalViewOmitsBlockedReasonAndReportsNilGoal(t *testing.T) {
	blocked := createTestGoal("obj", t0, nil)
	blocked.Phase = PhaseBlocked
	blocked.BlockedReason = &BlockedReason{Code: "stuck", Message: "no path"}
	g := GoalView{GoalSnapshot: blocked}
	result := GoalViewOf(&g)
	if result.Goal.BlockedReason == nil || result.Goal.BlockedReason.Message != "no path" || result.Activation != "disarmed" {
		t.Fatalf("unexpected result: %+v", result)
	}
	clean := GoalView{GoalSnapshot: createTestGoal("obj", t0, nil)}
	data, _ := json.Marshal(GoalViewOf(&clean))
	if strings.Contains(string(data), "blockedReason") {
		t.Fatalf("blockedReason must be omitted: %s", data)
	}
	nullData, _ := json.Marshal(GoalViewOf(nil))
	if string(nullData) != `{"goal":null}` {
		t.Fatalf("got %s", nullData)
	}
}

func TestResumeHintNamesTheFixForEachProviderPauseReason(t *testing.T) {
	cases := map[string]string{
		"api-auth":    "API key",
		"api-billing": "credits",
		"api-request": "request",
		"api-error":   "recovers",
	}
	for code, want := range cases {
		if got := ResumeHint(BlockedReason{Code: code, Message: "HTTP"}); !strings.Contains(got, want) {
			t.Fatalf("%s: got %q", code, got)
		}
	}
	// Unknown codes still produce an actionable line rather than nothing.
	if got := ResumeHint(BlockedReason{Code: "human-paused", Message: "Paused by user."}); !strings.Contains(got, "/goal resume") {
		t.Fatalf("got %q", got)
	}
}

func TestFoldIgnoresLegacyBannerSettingsEntries(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		{CustomType: "pi-goal-settings", Data: json.RawMessage(`{"bannerEnabled":true,"timestamp":` + itoa(t0+1) + `}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal == nil || folded.Goal.ID != g.ID {
		t.Fatalf("unexpected view: %+v", folded.Goal)
	}
}

func TestCreateGoalStateStampsTheStateVersion(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	if g.Version == nil || *g.Version != 1 {
		t.Fatalf("version %v", g.Version)
	}
}

func TestApplyChangeRejectsAnUnknownStateVersion(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	version := 2
	g.Version = &version
	if _, err := ApplyChange(nil, GoalChangeEntry{Operation: OpCreate, Goal: &g, Timestamp: t0}); err == nil || !strings.Contains(err.Error(), "unsupported goal state version") {
		t.Fatalf("err=%v", err)
	}
	// A missing version stays readable: entries written before the field existed are v1.
	legacy := createTestGoal("obj", t0, nil)
	legacy.Version = nil
	if _, err := ApplyChange(nil, GoalChangeEntry{Operation: OpCreate, Goal: &legacy, Timestamp: t0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoalRoundPromptFramesTheObjectiveAsUntrustedUserData(t *testing.T) {
	g := GoalView{GoalSnapshot: createTestGoal("delete every table in prod", t0, nil), Armed: true, TurnsStarted: 2}
	prompt := GoalRoundPrompt(g, 3, testTime(t0))
	for _, want := range []string{"<untrusted_objective>delete every table in prod</untrusted_objective>", "user-provided data", "Round 3."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// D239: the paused notice is model-facing context, so it carries the stop code,
// the reason, the objective as reference only, and how a human resumes.
func TestPauseContextTellsTheModelToStop(t *testing.T) {
	content := PauseContext("ship it", BlockedReason{Code: "budget-tokens", Message: "Token limit reached."})
	for _, want := range []string{"<goal_paused>", "</goal_paused>", "budget-tokens", "Token limit reached.", "ship it", "do not continue", "/goal resume"} {
		if !strings.Contains(content, want) {
			t.Fatalf("pause context missing %q:\n%s", want, content)
		}
	}
}

func TestChangeEntryJSONByteParity(t *testing.T) {
	g := createTestGoal("obj", t0, nil)
	entry := GoalChangeEntry{Operation: OpCreate, Goal: &g, Timestamp: t0}
	data, _ := json.Marshal(entry)
	expected := `{"operation":"create","goal":{"version":1,"id":"` + g.ID + `","revision":1,"objective":"obj","phase":"active","createdAt":1000,"updatedAt":1000},"timestamp":1000}`
	if string(data) != expected {
		t.Fatalf("got %s\nwant %s", data, expected)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
