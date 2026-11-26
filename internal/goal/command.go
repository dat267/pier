// This file follows goal/command.ts: the pure /goal argument parser.
package goal

import (
	"strings"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
)

// GoalCommandKind names one parsed /goal intent.
type GoalCommandKind string

// The parsed command kinds.
const (
	CmdShowStatus GoalCommandKind = "show_status"
	CmdClear      GoalCommandKind = "clear"
	CmdPause      GoalCommandKind = "pause"
	CmdResume     GoalCommandKind = "resume"
	CmdSet        GoalCommandKind = "set"
	CmdError      GoalCommandKind = "error"
)

// GoalCommand is one parsed /goal intent.
type GoalCommand struct {
	Kind      GoalCommandKind
	Objective string
	Message   string
}

// GoalCommandCompletions are the accepted subcommands, in order.
var GoalCommandCompletions = []string{"status", "set", "pause", "resume", "clear"}

// ParseGoalCommand parses the raw /goal argument string.
func ParseGoalCommand(args string) GoalCommand {
	trimmed := ai.JSTrim(args)

	if trimmed == "" {
		return GoalCommand{Kind: CmdError, Message: "Usage: /goal status | /goal set <objective> | /goal pause | /goal resume | /goal clear"}
	}
	switch trimmed {
	case "status":
		return GoalCommand{Kind: CmdShowStatus}
	case "clear":
		return GoalCommand{Kind: CmdClear}
	case "pause":
		return GoalCommand{Kind: CmdPause}
	case "resume":
		return GoalCommand{Kind: CmdResume}
	}

	// Creation requires the explicit "set" verb — any other unknown word is a
	// typo, not an objective (e.g. "/goal view", "/goal cleared"). The verb is
	// followed by whitespace, not by a literal space: the argument arrives raw and
	// JSTrim already treats tab and newline as whitespace, so requiring only " "
	// reported "/goal set<TAB>x" as an unknown subcommand.
	if !hasSetVerb(trimmed) {
		return GoalCommand{Kind: CmdError, Message: `Unknown subcommand "` + TruncateObjective(trimmed, 20) + `". Use /goal status, /goal set <objective>, /goal pause, /goal resume, or /goal clear.`}
	}

	objective := ai.JSTrim(trimmed[3:])
	if objective == "" {
		return GoalCommand{Kind: CmdError, Message: "Usage: /goal set <objective>"}
	}
	if strings.HasPrefix(objective, "--") {
		return GoalCommand{Kind: CmdError, Message: "Goal budgets are not supported. Use /goal set <objective>."}
	}
	return GoalCommand{Kind: CmdSet, Objective: objective}
}

// hasSetVerb reports whether trimmed is "set" alone or "set" followed by a
// whitespace rune, using the same JS whitespace set JSTrim uses.
func hasSetVerb(trimmed string) bool {
	if trimmed == "set" {
		return true
	}
	if !strings.HasPrefix(trimmed, "set") {
		return false
	}
	rest := trimmed[3:]
	if rest == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return ai.IsJSSpace(r)
}
