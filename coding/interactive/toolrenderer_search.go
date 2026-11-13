package interactive

import (
	"strings"

	"github.com/dat267/pier/tui"
)

func formatSearchResult(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme,
	showImages bool, maxLines int) string {
	output := strings.TrimSpace(GetTextOutput(result, showImages))
	text := appendTruncatedLines("", output, expandedMaxLines(options, output, maxLines), theme)
	return text + truncationSuffix(result.Details, theme)
}

// --- grep / find ------------------------------------------------------------

func formatGrepCall(args map[string]any, theme *Theme) string {
	pattern, patternOK := argString(args, "pattern")
	rawPath, pathOK := argString(args, "path")
	glob, _ := argString(args, "glob")
	limit, hasLimit := argNumber(args, "limit")
	invalidArg := invalidArgText(theme)
	path := "."
	if !pathOK {
		path = ""
	} else if rawPath != "" {
		path = ShortenPath(rawPath)
	}
	text := theme.Fg("toolTitle", theme.Bold("grep")) + " "
	if !patternOK {
		text += invalidArg
	} else {
		text += theme.Fg("accent", "/"+pattern+"/")
	}
	if !pathOK {
		text += theme.Fg("toolOutput", " in "+invalidArg)
	} else {
		text += theme.Fg("toolOutput", " in "+path)
	}
	if glob != "" {
		text += theme.Fg("toolOutput", " ("+glob+")")
	}
	if hasLimit {
		text += theme.Fg("toolOutput", " limit "+formatToolLimit(limit))
	}
	return text
}

var grepRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(formatGrepCall(toolArgs(args), theme))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(formatSearchResult(result, options, theme, context.ShowImages, 15))
		return text
	},
}

func formatFindCall(args map[string]any, theme *Theme) string {
	pattern, patternOK := argString(args, "pattern")
	rawPath, pathOK := argString(args, "path")
	limit, hasLimit := argNumber(args, "limit")
	invalidArg := invalidArgText(theme)
	text := theme.Fg("toolTitle", theme.Bold("find")) + " "
	if !patternOK {
		text += invalidArg
	} else {
		text += theme.Fg("accent", pattern)
	}
	if !pathOK {
		text += theme.Fg("toolOutput", " in "+invalidArg)
	} else {
		shown := "."
		if rawPath != "" {
			shown = ShortenPath(rawPath)
		}
		text += theme.Fg("toolOutput", " in "+shown)
	}
	if hasLimit {
		text += theme.Fg("toolOutput", " (limit "+formatToolLimit(limit)+")")
	}
	return text
}

var findRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(formatFindCall(toolArgs(args), theme))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(formatSearchResult(result, options, theme, context.ShowImages, 20))
		return text
	},
}
