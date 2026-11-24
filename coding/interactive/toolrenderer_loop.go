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
		title := theme.Fg("toolTitle", theme.Bold("cron_create"))
		label := humanScheduleForDisplay(cron)
		if label == "" {
			label = "(no schedule)"
		}
		text.SetText(title + " " + theme.Fg("toolOutput", label) + recurringSuffix(recurring, theme) +
			"\n  " + theme.Fg("toolOutput", firstLineEllipsis(prompt, 100)))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(renderPlainResult(result, options, theme, context))
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
		shown := truncatedOutputLines(output, expandedMaxLines(options, output, 20), theme)
		text.SetText(shown + truncationSuffix(result.Details, theme))
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

// humanScheduleForDisplay names a cron expression the way the tool reports it.
func humanScheduleForDisplay(cron string) string {
	if strings.TrimSpace(cron) == "" {
		return ""
	}
	return coding.HumanCronSchedule(cron)
}

// recurringSuffix labels a one-shot job, since recurring is the default and saying
// so on every row is noise.
func recurringSuffix(recurring bool, theme *Theme) string {
	if recurring {
		return ""
	}
	return theme.Fg("toolOutput", " (once)")
}

// firstLineEllipsis keeps a prompt readable in a one-line frame.
func firstLineEllipsis(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index]) + " …"
	}
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit]) + "…"
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
