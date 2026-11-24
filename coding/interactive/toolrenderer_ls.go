package interactive

import (
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

func formatLsCall(args map[string]any, theme *Theme, cwd string) string {
	rawPath, _ := argString(args, "path")
	pathDisplay := RenderToolPath(&rawPath, theme, cwd, ".")
	text := theme.Fg("toolTitle", theme.Bold("ls")) + " " + pathDisplay
	// Upstream tests `limit !== undefined` and interpolates the raw value, so a
	// string or null limit is labelled too.
	if limit, ok := args["limit"]; ok {
		text += theme.Fg("toolOutput", " (limit "+ai.JSValueToString(limit)+")")
	}
	return text
}

var lsRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(formatLsCall(toolArgs(args), theme, context.Cwd))
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		output := strings.TrimSpace(GetTextOutput(result, context.ShowImages))
		rendered := truncatedOutputLines(output, expandedMaxLines(options, output, 20), theme)
		text.SetText(rendered + truncationSuffix(result.Details, theme))
		return text
	},
}
