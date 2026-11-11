package coding

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D223: the goal is durable branch state, replayed from the branch at
// startup. Navigating the session tree moves the branch, so the goal must be
// the one the new branch holds: a goal set after the navigation target is not
// on the new branch, and a command after navigating must not persist a
// transition against it, or the branch no longer replays.
func TestGoalFollowsTheBranchAcrossTreeNavigation(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: stubStreamFn, Goal: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	session.Goal.SetStartWork(func(func(context.Context) error) {})

	if err := session.Prompt(context.Background(), "before the goal", nil); err != nil {
		t.Fatal(err)
	}
	forks := session.GetUserMessagesForForking()
	if len(forks) != 1 {
		t.Fatalf("fork targets = %+v", forks)
	}
	session.Goal.HandleCommand("set ship the release")
	if session.Goal.Snapshot().Goal == nil {
		t.Fatal("the goal was not set")
	}

	// Back to before the goal existed: the branch holds no goal now.
	if _, err := session.NavigateTree(context.Background(), forks[0].EntryID, NavigateTreeOptions{}); err != nil {
		t.Fatal(err)
	}
	if goal := session.Goal.Snapshot().Goal; goal != nil {
		t.Fatalf("after navigating to before the goal, the controller still holds %q", goal.Objective)
	}

	// A command now must persist against the branch's goal state, and the
	// branch must replay without a corruption warning.
	session.Goal.HandleCommand("set a different objective")
	var warnings []string
	session.Goal.SetUIHooks(func(message, level string) {
		if level == "warning" && strings.Contains(message, "corrupt") {
			warnings = append(warnings, message)
		}
	})
	session.Goal.Start("startup")
	if len(warnings) > 0 {
		t.Fatalf("the branch does not replay after navigating: %v", warnings)
	}
	if goal := session.Goal.Snapshot().Goal; goal == nil || goal.Objective != "a different objective" {
		t.Fatalf("replayed goal = %+v", goal)
	}
}
