package goal

import (
	"strings"
	"testing"
)

// Mirrors ~/.pi/agent/extensions/goal/command.test.ts.

func TestParseGoalCommandBareGoalPrintsUsage(t *testing.T) {
	for _, input := range []string{"", "   "} {
		result := ParseGoalCommand(input)
		if result.Kind != CmdError || !strings.Contains(result.Message, "Usage: /goal status") {
			t.Fatalf("%q: %+v", input, result)
		}
	}
}

func TestParseGoalCommandStatus(t *testing.T) {
	if got := ParseGoalCommand("status"); got.Kind != CmdShowStatus {
		t.Fatalf("got %+v", got)
	}
}

func TestParseGoalCommandRemovesBannerAndExpand(t *testing.T) {
	for _, input := range []string{"banner", "expand"} {
		result := ParseGoalCommand(input)
		if result.Kind != CmdError || !strings.Contains(result.Message, "Unknown subcommand") {
			t.Fatalf("%q: %+v", input, result)
		}
	}
}

func TestParseGoalCommandLifecycleSubcommands(t *testing.T) {
	cases := map[string]GoalCommandKind{"clear": CmdClear, "pause": CmdPause, "resume": CmdResume}
	for input, want := range cases {
		if got := ParseGoalCommand(input); got.Kind != want {
			t.Fatalf("%q: got %+v", input, got)
		}
	}
}

func TestParseGoalCommandSetsAPlainObjective(t *testing.T) {
	got := ParseGoalCommand("set test objective")
	if got.Kind != CmdSet || got.Objective != "test objective" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseGoalCommandRejectsBudgetFlags(t *testing.T) {
	for _, input := range []string{"set --time 8h task", "set --tokens 500k task", "set --cost $10 task"} {
		result := ParseGoalCommand(input)
		if result.Kind != CmdError || !strings.Contains(result.Message, "/goal set <objective>") {
			t.Fatalf("%q: %+v", input, result)
		}
	}
}

func TestParseGoalCommandSetWithoutObjective(t *testing.T) {
	result := ParseGoalCommand("set   ")
	if result.Kind != CmdError || !strings.Contains(result.Message, "Usage: /goal set") {
		t.Fatalf("got %+v", result)
	}
}

func TestParseGoalCommandUnknownSubcommand(t *testing.T) {
	result := ParseGoalCommand("view")
	if result.Kind != CmdError || !strings.Contains(result.Message, `Unknown subcommand "view"`) {
		t.Fatalf("got %+v", result)
	}
}

func TestGoalCommandCompletions(t *testing.T) {
	want := []string{"status", "set", "pause", "resume", "clear"}
	if len(GoalCommandCompletions) != len(want) {
		t.Fatalf("got %v", GoalCommandCompletions)
	}
	for i, value := range want {
		if GoalCommandCompletions[i] != value {
			t.Fatalf("got %v", GoalCommandCompletions)
		}
	}
}
