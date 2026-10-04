package goal

import "strings"

// This file follows goal/command.ts: the pure /goal argument parser.

// GoalCommandKind identifies a parsed /goal intent.
type GoalCommandKind string

// The parsed command kinds.
const (
	CmdToggleBanner GoalCommandKind = "toggle_banner"
	CmdShowStatus   GoalCommandKind = "show_status"
	CmdClear        GoalCommandKind = "clear"
	CmdPause        GoalCommandKind = "pause"
	CmdResume       GoalCommandKind = "resume"
	CmdSet          GoalCommandKind = "set"
	CmdError        GoalCommandKind = "error"
)

// GoalCommand is a parsed /goal intent.
type GoalCommand struct {
	Kind      GoalCommandKind
	Objective string
	Message   string
}

// ParseGoalCommand parses the raw /goal argument string.
func ParseGoalCommand(args string) GoalCommand {
	trimmed := strings.TrimSpace(args)

	switch {
	case trimmed == "":
		return GoalCommand{Kind: CmdToggleBanner}
	case trimmed == "status":
		return GoalCommand{Kind: CmdShowStatus}
	case trimmed == "banner":
		return GoalCommand{Kind: CmdToggleBanner}
	case trimmed == "clear":
		return GoalCommand{Kind: CmdClear}
	case trimmed == "pause":
		return GoalCommand{Kind: CmdPause}
	case trimmed == "resume":
		return GoalCommand{Kind: CmdResume}
	}

	// Creation requires the explicit "set" verb — any other unknown word is a
	// typo, not an objective (e.g. "/goal view", "/goal cleared").
	if !strings.HasPrefix(trimmed, "set ") && trimmed != "set" {
		return GoalCommand{
			Kind: CmdError,
			Message: `Unknown subcommand "` + TruncateObjective(trimmed, 20) + `". Use /goal set <objective>, ` +
				`/goal status, pause, resume, clear, or bare /goal to toggle the banner.`,
		}
	}

	objective := strings.TrimSpace(trimmed[3:])
	if objective == "" {
		return GoalCommand{Kind: CmdError, Message: "Usage: /goal set <objective>"}
	}
	return GoalCommand{Kind: CmdSet, Objective: objective}
}
