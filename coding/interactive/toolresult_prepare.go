package interactive

import (
	"encoding/json"
	"reflect"
	"slices"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// SetResultPreparation opts built-in tool results into the same owner-applied
// submission protocol as Markdown. Custom renderer callbacks remain on-owner.
func (c *ToolExecutionComponent) SetResultPreparation(preparation *tui.MarkdownPreparation) {
	if c.frames.SetPreparation(preparation) {
		c.updateDisplay()
	}
}

// Prepare warms only detached tools synchronously, without nested submissions.
func (c *ToolExecutionComponent) Prepare(width int) { c.frames.Prepare(width, c.updateDisplay) }

// D205: capture only known built-in values. Rendering lifecycle state and
// publication belong to toolFrames; this function only constructs its input.
func (c *ToolExecutionComponent) captureBuiltinResult(theme *Theme, context *ToolRenderContext) *toolFrameInput {
	if !c.frames.Enabled() || c.definition == nil || c.definition.RenderResult == nil || c.result == nil {
		return nil
	}
	renderer := reflect.ValueOf(c.definition.RenderResult).Pointer()
	read := renderer == reflect.ValueOf(readRenderers.RenderResult).Pointer()
	shell := renderer == reflect.ValueOf(bashRenderers.RenderResult).Pointer()
	if (!read && !shell) || (read && !c.expanded) {
		return nil
	}
	size := 0
	for _, block := range c.result.Content {
		if block.Type != "text" {
			return nil
		}
		size += len(block.Text)
	}
	if size < 64*1024 {
		return nil
	}
	result := *c.result
	result.Content = slices.Clone(result.Content)
	switch details := result.Details.(type) {
	case json.RawMessage:
		if len(details) > 16*1024 {
			return nil
		}
		result.Details = slices.Clone(details)
	case []byte:
		if len(details) > 16*1024 {
			return nil
		}
		result.Details = slices.Clone(details)
	default:
		if result.Details != nil {
			if read {
				details, ok := result.Details.(*coding.ReadToolDetails)
				if !ok || details == nil {
					return nil
				}
				copy := *details
				copy.Truncation = cloneToolTruncation(details.Truncation)
				result.Details = &copy
			} else {
				switch result.Details.(type) {
				case coding.BashToolDetails, *coding.BashToolDetails:
				default:
					return nil
				}
				details := toolDetailsFrom[coding.BashToolDetails](result.Details)
				if details == nil {
					return nil
				}
				copy := *details
				copy.Truncation = cloneToolTruncation(details.Truncation)
				if details.FullOutputPath != nil {
					path := *details.FullOutputPath
					copy.FullOutputPath = &path
				}
				result.Details = &copy
			}
		}
	}
	path, _ := argString(toolArgs(c.args), "file_path", "path")
	args := map[string]any{"path": path}
	theme = theme.concrete()
	showImages := c.showImages
	options := ToolRenderResultOptions{Expanded: c.expanded, IsPartial: c.isPartial}
	expandHint := KeyHint("app.tools.expand", "to expand")
	input := &toolFrameInput{theme: theme, background: "toolSuccessBg"}
	input.build = func() tui.Component {
		if read {
			return tui.NewText(formatReadResult(args, &result, options, theme, showImages, result.IsError), 0, 0, nil)
		}
		container := &tui.Container{}
		for _, child := range rebuildBashResult(&result, options, theme, showImages, &bashResultState{}) {
			if preview, ok := child.(*bashPreviewComponent); ok {
				preview.expandHint = expandHint
			}
			container.AddChild(child)
		}
		return container
	}
	if shell {
		state := shellCallStateFor(context)
		if state.startedAtMS != 0 {
			if state.endedAtMS == 0 && (!options.IsPartial || result.IsError) {
				state.endedAtMS = shellNow().UnixMilli()
			}
			input.tail = &shellElapsedComponent{state: state, theme: theme}
		}
	}
	if c.isPartial {
		input.background = "toolPendingBg"
	} else if c.result.IsError {
		input.background = "toolErrorBg"
	}
	if c.renderShell() != "self" && c.definition.RenderCall != nil {
		call := reflect.ValueOf(c.definition.RenderCall).Pointer()
		input.boxed = call == reflect.ValueOf(readRenderers.RenderCall).Pointer() || call == reflect.ValueOf(bashRenderers.RenderCall).Pointer()
	}
	return input
}
func cloneToolTruncation(value *coding.TruncationResult) *coding.TruncationResult {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
