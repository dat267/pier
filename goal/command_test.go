package goal

import (
	"strings"
	"testing"
)

// Mirrors goal/command.test.ts.

func TestParseGoalCommand(t *testing.T) {
	cases := []struct {
		args string
		kind GoalCommandKind
	}{
		{"", CmdToggleBanner},
		{"   ", CmdToggleBanner},
		{"banner", CmdToggleBanner},
		{"status", CmdShowStatus},
		{"clear", CmdClear},
		{"pause", CmdPause},
		{"resume", CmdResume},
	}
	for _, testCase := range cases {
		if got := ParseGoalCommand(testCase.args); got.Kind != testCase.kind {
			t.Fatalf("parse(%q) = %s, want %s", testCase.args, got.Kind, testCase.kind)
		}
	}

	if got := ParseGoalCommand("set test objective"); got.Kind != CmdSet || got.Objective != "test objective" {
		t.Fatalf("set = %+v", got)
	}

	usage := ParseGoalCommand("set   ")
	if usage.Kind != CmdError || !strings.Contains(usage.Message, "Usage: /goal set") {
		t.Fatalf("usage = %+v", usage)
	}

	unknown := ParseGoalCommand("view")
	if unknown.Kind != CmdError || !strings.Contains(unknown.Message, `Unknown subcommand "view"`) {
		t.Fatalf("unknown = %+v", unknown)
	}
}
