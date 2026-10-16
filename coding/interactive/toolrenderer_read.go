package interactive

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

type compactReadClassification struct {
	kind  string // "docs" | "resource" | "skill"
	label string
}

var compactResourceFileNames = map[string]bool{
	"AGENTS.override.md": true, "AGENTS.md": true, "AGENTS.MD": true,
	"CLAUDE.md": true, "CLAUDE.MD": true,
}

func styledReadLineRange(args map[string]any, theme *Theme) string {
	rangeText := formatReadLineRange(args["offset"], args["limit"])
	if rangeText == "" {
		return ""
	}
	return theme.Fg("warning", rangeText)
}

func formatReadCall(args map[string]any, theme *Theme, cwd string) string {
	rawPath, _ := argString(args, "file_path", "path")
	pathDisplay := RenderToolPath(&rawPath, theme, cwd, "")
	return theme.Fg("toolTitle", theme.Bold("read")) + " " + pathDisplay + styledReadLineRange(args, theme)
}

func getPiDocsClassification(absolutePath string) *compactReadClassification {
	packageRoot := filepath.Dir(coding.GetReadmePath())
	relativePath, err := filepath.Rel(packageRoot, absolutePath)
	if err != nil {
		return nil
	}
	if relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relativePath) {
		return nil
	}
	label := toPosixPath(relativePath)
	if label == "README.md" || strings.HasPrefix(label, "docs/") || strings.HasPrefix(label, "examples/") {
		return &compactReadClassification{kind: "docs", label: label}
	}
	return nil
}

func getCompactReadClassification(args map[string]any, cwd string) *compactReadClassification {
	rawPath, ok := argString(args, "file_path", "path")
	if !ok || rawPath == "" {
		return nil
	}
	absolutePath := coding.ResolveToCwd(rawPath, cwd)
	fileName := filepath.Base(absolutePath)
	if fileName == "SKILL.md" {
		label := filepath.Base(filepath.Dir(absolutePath))
		if label == "" || label == "." || label == string(filepath.Separator) {
			label = fileName
		}
		return &compactReadClassification{kind: "skill", label: label}
	}
	if docs := getPiDocsClassification(absolutePath); docs != nil {
		return docs
	}
	if compactResourceFileNames[fileName] {
		return &compactReadClassification{kind: "resource", label: formatPathRelativeToCwdOrAbsolute(absolutePath, cwd)}
	}
	return nil
}

func formatCompactReadCall(classification compactReadClassification, args map[string]any, theme *Theme) string {
	expandHint := theme.Fg("dim", " ("+KeyDisplayText("app.tools.expand")+" to expand)")
	if classification.kind == "skill" {
		return theme.Fg("customMessageLabel", "\x1b[1m[skill]\x1b[22m ") +
			theme.Fg("customMessageText", classification.label) +
			styledReadLineRange(args, theme) + expandHint
	}
	return theme.Fg("toolTitle", theme.Bold("read "+classification.kind)) +
		" " + theme.Fg("accent", classification.label) +
		styledReadLineRange(args, theme) + expandHint
}

func formatReadResult(args map[string]any, result *SortToolResultContent, options ToolRenderResultOptions,
	theme *Theme, showImages bool, isError bool) string {
	if !options.Expanded && !isError {
		return ""
	}
	rawPath, _ := argString(args, "file_path", "path")
	output := GetTextOutput(result, showImages)
	lang := ""
	hasLang := false
	if !isError && rawPath != "" {
		lang, hasLang = GetLanguageFromPath(rawPath)
	}
	var lines []string
	if hasLang {
		lines = highlightCode(replaceTabs(output), lang, theme)
	} else {
		lines = strings.Split(output, "\n")
	}
	lines = trimTrailingEmptyLines(lines)
	maxLines := 10
	if options.Expanded {
		maxLines = len(lines)
	}
	displayLines := lines
	if len(displayLines) > maxLines {
		displayLines = displayLines[:maxLines]
	}
	remaining := len(lines) - len(displayLines)
	parts := make([]string, 0, len(displayLines))
	for _, line := range displayLines {
		if hasLang {
			parts = append(parts, replaceTabs(line))
		} else {
			parts = append(parts, theme.Fg("toolOutput", replaceTabs(line)))
		}
	}
	text := "\n" + strings.Join(parts, "\n")
	if remaining > 0 {
		text += theme.Fg("muted", fmt.Sprintf("\n... (%d more lines,", remaining)) +
			" " + KeyHint("app.tools.expand", "to expand") + theme.Fg("muted", ")")
	}
	if details, ok := result.Details.(*coding.ReadToolDetails); ok && details.Truncation != nil {
		truncation := details.Truncation
		if truncation.Truncated {
			maxBytes := truncation.MaxBytes
			if maxBytes <= 0 {
				maxBytes = coding.DefaultMaxBytes
			}
			if truncation.FirstLineExceedsLimit {
				text += "\n" + theme.Fg("warning",
					fmt.Sprintf("[First line exceeds %s limit]", coding.FormatSize(int64(maxBytes))))
			} else if truncation.TruncatedBy == "lines" {
				maxLines := truncation.MaxLines
				if maxLines <= 0 {
					maxLines = coding.DefaultMaxLines
				}
				text += "\n" + theme.Fg("warning", fmt.Sprintf(
					"[Truncated: showing %d of %d lines (%d line limit)]",
					truncation.OutputLines, truncation.TotalLines, maxLines))
			} else {
				text += "\n" + theme.Fg("warning", fmt.Sprintf(
					"[Truncated: %d lines shown (%s limit)]", truncation.OutputLines,
					coding.FormatSize(int64(maxBytes))))
			}
		}
	}
	return text
}

var readRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		decoded := toolArgs(args)
		text := toolTextComponent(context)
		classification := (*compactReadClassification)(nil)
		if context != nil && !context.Expanded {
			classification = getCompactReadClassification(decoded, context.Cwd)
		}
		if classification != nil {
			text.SetText(formatCompactReadCall(*classification, decoded, theme))
		} else {
			text.SetText(formatReadCall(decoded, theme, context.Cwd))
		}
		return text
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		text := toolTextComponent(context)
		text.SetText(formatReadResult(toolArgs(context.Args), result, options, theme, context.ShowImages, context.IsError))
		return text
	},
}

// --- ls ---------------------------------------------------------------------
