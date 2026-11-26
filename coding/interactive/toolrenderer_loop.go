package interactive

// The scheduled-prompt tools' transcript frames. They are pier-only tools (D537),
// so they have no reference renderer; without one they fell through to the
// generic MCP frame, which dumped the raw argument JSON. These render what the
// call means: the schedule, and the prompt it will run.

import (
	"strconv"
	"strings"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// cronCreateRenderers renders cron_create: the schedule and the prompt, not the
// argument object.
var cronCreateRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		argsMap := toolArgs(args)
		cron, _ := argString(argsMap, "cron")
		prompt, _ := argString(argsMap, "prompt")
		recurring := true
		if value, ok := argsMap["recurring"].(bool); ok {
			recurring = value
		}
		// The shell/MCP convention: a bold title, then the arguments in the muted
		// style on the same line, untruncated. The frame wraps if it is long rather
		// than cutting the user's own text, which is what formatMcpCall and
		// formatShellCall do.
		schedule := humanScheduleForDisplay(cron)
		if schedule == "" {
			schedule = "(no schedule)"
		}
		// A prompt is one argument, so a newline in it is flattened: an embedded
		// line break would push the rest of the frame onto its own line and read as
		// a second argument.
		parts := []string{"schedule=" + schedule}
		if !recurring {
			parts = append(parts, "once")
		}
		parts = append(parts, "prompt="+singleLine(prompt))
		text.SetText(theme.Fg("toolTitle", theme.Bold("cron_create")) + " " +
			theme.Fg("muted", strings.Join(parts, " ")))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		// The model-facing text repeats the schedule the call frame already shows,
		// so the transcript gets the outcome alone. An error keeps its text.
		text := toolTextComponent(context)
		if result.IsError {
			text.SetText(renderPlainResult(result, options, theme, context))
			return text
		}
		// The model-facing text repeats the schedule the call frame already shows,
		// so a successful create draws no body at all.
		text.SetText("")
		return text
	},
}

// cronDeleteRenderers renders cron_delete: which job is being cancelled.
var cronDeleteRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		id, _ := argString(toolArgs(args), "id")
		text.SetText(theme.Fg("toolTitle", theme.Bold("cron_delete")) + " " +
			theme.Fg("toolOutput", "job "+id))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(renderPlainResult(result, options, theme, context))
		return text
	},
}

// cronListRenderers renders cron_list: one row per job, the schedule first.
var cronListRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(theme.Fg("toolTitle", theme.Bold("cron_list")))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		output := strings.TrimSpace(GetTextOutput(result, context.ShowImages))
		if output == "" {
			text.SetText(theme.Fg("toolOutput", "(no scheduled jobs)"))
			return text
		}
		var lines []string
		for _, line := range strings.Split(output, "\n") {
			lines = append(lines, compactJobRow(line))
		}
		joined := strings.Join(lines, "\n")
		shown := truncatedOutputLines(joined, expandedMaxLines(options, joined, 20), theme)
		text.SetText(strings.TrimLeft(shown, "\n") + truncationSuffix(result.Details, theme))
		return text
	},
}

// scheduleWakeupRenderers renders schedule_wakeup: the delay it asked for, or the
// stop.
var scheduleWakeupRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		argsMap := toolArgs(args)
		title := theme.Fg("toolTitle", theme.Bold("schedule_wakeup"))
		if stop, _ := argsMap["stop"].(bool); stop {
			text.SetText(title + " " + theme.Fg("toolOutput", "stop"))
			return text
		}
		delay := ""
		if seconds, ok := argsMap["delay_seconds"].(float64); ok && seconds > 0 {
			delay = humanDelay(time.Duration(seconds) * time.Second)
		}
		text.SetText(title + " " + theme.Fg("toolOutput", delay))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(renderPlainResult(result, options, theme, context))
		return text
	},
}

// compactJobRow shortens one job row for a frame: the next-run timestamp is noise
// next to the schedule, and the prompt is cut so a job stays one line.
func compactJobRow(row string) string {
	trimmed := strings.TrimSpace(row)
	if trimmed == "" {
		return ""
	}
	// "<id> — <schedule> (<kind>), next run <time>: <prompt>"
	if at := strings.Index(trimmed, ", next run "); at >= 0 {
		rest := trimmed[at:]
		if colon := strings.Index(rest, ": "); colon >= 0 {
			trimmed = trimmed[:at] + ": " + singleLine(rest[colon+2:])
		}
	}
	return trimmed
}

// humanScheduleForDisplay names a cron expression the way the tool reports it.
func humanScheduleForDisplay(cron string) string {
	if strings.TrimSpace(cron) == "" {
		return ""
	}
	return coding.HumanCronSchedule(cron)
}

// singleLine flattens a prompt's whitespace so it renders as one argument. The
// shell and MCP frames never truncate their arguments, so neither does this: the
// frame wraps rather than cutting the user's text.
func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// humanDelay renders a wakeup delay.
func humanDelay(delay time.Duration) string {
	switch {
	case delay >= time.Hour && delay%time.Hour == 0:
		return plural(int(delay/time.Hour), "hour")
	case delay >= time.Minute:
		return plural(int(delay/time.Minute), "minute")
	default:
		return plural(int(delay/time.Second), "second")
	}
}

// plural renders "<n> <unit>", with an s for anything but one.
func plural(count int, unit string) string {
	if count == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(count) + " " + unit + "s"
}

// renderPlainResult is a result frame for a tool whose result is one sentence: the
// text itself, trimmed, with an error kept visible.
func renderPlainResult(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) string {
	output := strings.TrimSpace(GetTextOutput(result, context.ShowImages))
	if output == "" {
		if result.IsError {
			return theme.Fg("toolOutput", "(failed)")
		}
		return ""
	}
	rendered := truncatedOutputLines(output, expandedMaxLines(options, output, 8), theme)
	return rendered + truncationSuffix(result.Details, theme)
}
