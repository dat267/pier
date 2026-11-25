package interactive

import (
	"strconv"
	"strings"
	"time"

	"github.com/dat267/pier/tui"
)

// Port of src/modes/interactive/components/custom-editor.ts: the editor with
// the app-level keybindings and the embedded working-status border.
//
// Go has no subclassing, so the editor's top-border override is a field hook
// (tui.Editor.TopBorder) and the app key handling lives in
// CustomEditor.HandleInput (callers hold the wrapper, not the embedded editor).

// CustomEditorOptions configure the custom editor.
type CustomEditorOptions struct {
	PaddingX               int
	AutocompleteMaxVisible int
	// EmbedWorkingStatus renders the working status in the editor's top border.
	EmbedWorkingStatus bool
}

// CustomEditor wraps the TUI editor with the coding-agent keybindings.
type CustomEditor struct {
	*tui.Editor

	keybindings            *AppKeybindingsManager
	workingStatusIndicator *StatusIndicator
	// LoopCount and BackgroundCalls report the scheduled /loop jobs (D537) and
	// the backgrounded tool calls (D538), which the lower divider counts; nil
	// shows none.
	LoopCount       func() int
	BackgroundCalls func() int

	// EmbedWorkingStatus reports the border-status option.
	EmbedWorkingStatus bool
	// ActionHandlers maps app keybindings to handlers.
	ActionHandlers map[tui.Keybinding]func()
	// actionOrderKeys is the registration order, which the reference keeps
	// through its Map; the first registered action wins a shared key.
	actionOrderKeys []tui.Keybinding

	OnEscape            func()
	OnCtrlD             func()
	OnPasteImage        func()
	OnExtensionShortcut func(data string) bool
}

// NewCustomEditor creates the editor.
func NewCustomEditor(host tui.EditorHost, theme tui.EditorTheme, keybindings *AppKeybindingsManager, options CustomEditorOptions) *CustomEditor {
	editor := tui.NewEditor(host, theme, tui.EditorOptions{
		PaddingX:               options.PaddingX,
		AutocompleteMaxVisible: options.AutocompleteMaxVisible,
	})
	custom := &CustomEditor{
		Editor:             editor,
		keybindings:        keybindings,
		EmbedWorkingStatus: options.EmbedWorkingStatus,
		ActionHandlers:     map[tui.Keybinding]func(){},
	}
	editor.TopBorder = custom.renderTopBorder
	editor.BottomBorder = custom.renderBottomBorder
	return custom
}

// EmbedWorkingStatusValue reports the embed option (WorkingStatusEditor).
func (c *CustomEditor) EmbedWorkingStatusValue() bool { return c.EmbedWorkingStatus }

// WorkingBorderColor returns the editor's border color (WorkingStatusEditor).
func (c *CustomEditor) WorkingBorderColor() func(string) string { return c.BorderColor }

// SetWorkingStatusIndicator sets the embedded status indicator.
func (c *CustomEditor) SetWorkingStatusIndicator(indicator *StatusIndicator) {
	c.workingStatusIndicator = indicator
}

// GetWorkingStatusIndicator returns the embedded status indicator.
func (c *CustomEditor) GetWorkingStatusIndicator() *StatusIndicator {
	return c.workingStatusIndicator
}

// AnimationFrame implements tui.Animator for the embedded working/compaction
// status. The indicator lives in the editor's border, not in a container, so
// the renderer's animation walk can only reach it through the editor; without
// this, the spinner freezes whenever nothing else repaints (e.g. during
// compaction, where no events stream).
func (c *CustomEditor) AnimationFrame(now time.Time) (bool, time.Duration) {
	if c.workingStatusIndicator == nil {
		return false, 0
	}
	return c.workingStatusIndicator.AnimationFrame(now)
}

// OnAction registers a handler for an app action.
func (c *CustomEditor) OnAction(action tui.Keybinding, handler func()) {
	if _, exists := c.ActionHandlers[action]; !exists {
		c.actionOrderKeys = append(c.actionOrderKeys, action)
	}
	c.ActionHandlers[action] = handler
}

// HandleInput processes the editor input with the app keybindings.
func (c *CustomEditor) HandleInput(data string) {
	if c.OnExtensionShortcut != nil && c.OnExtensionShortcut(data) {
		return
	}

	// Selection page keys move an open autocomplete menu before app actions
	// run (upstream custom-editor.ts), so a page key bound to an app action
	// still pages the menu while it is open.
	if c.Editor.IsShowingAutocomplete() && c.keybindings != nil &&
		(c.keybindings.Matches(data, "tui.select.pageUp") || c.keybindings.Matches(data, "tui.select.pageDown")) {
		c.Editor.HandleInput(data)
		return
	}

	if c.keybindings != nil && c.keybindings.Matches(data, "app.clipboard.pasteImage") {
		if c.OnPasteImage != nil {
			c.OnPasteImage()
		}
		return
	}

	if c.keybindings != nil && c.keybindings.Matches(data, "app.interrupt") {
		if !c.Editor.IsShowingAutocomplete() {
			handler := c.OnEscape
			if handler == nil {
				handler = c.ActionHandlers["app.interrupt"]
			}
			if handler != nil {
				handler()
				return
			}
		}
		c.Editor.HandleInput(data)
		return
	}

	if c.keybindings != nil && c.keybindings.Matches(data, "app.exit") {
		if len(c.Editor.GetText()) == 0 {
			handler := c.OnCtrlD
			if handler == nil {
				handler = c.ActionHandlers["app.exit"]
			}
			if handler != nil {
				handler()
			}
			return
		}
		// Fall through for delete-char-forward when not empty.
	}

	if c.keybindings != nil &&
		(c.keybindings.Matches(data, "tui.editor.historyPrevious") ||
			c.keybindings.Matches(data, "tui.editor.historyNext")) {
		c.Editor.HandleInput(data)
		return
	}

	// Iterate in registration order, as the reference iterates its Map.
	for _, action := range c.actionOrder() {
		if action == "app.interrupt" || action == "app.exit" {
			continue
		}
		if c.keybindings != nil && c.keybindings.Matches(data, action) {
			c.ActionHandlers[action]()
			return
		}
	}

	c.Editor.HandleInput(data)
}

// actionOrder returns the registered actions in registration order (the
// reference's Map insertion order, which a Go map does not preserve).
func (c *CustomEditor) actionOrder() []tui.Keybinding {
	return c.actionOrderKeys
}

// renderBottomBorder puts the counts of scheduled loops (D537) and backgrounded
// calls (D538) in the lower divider, above the footer, which holds only the
// context, the model and the directory. It gives way to the scroll indicator,
// which says there is more of the draft below, and to a width too narrow for the
// label.
func (c *CustomEditor) renderBottomBorder(width int, hiddenLineCount int) string {
	var parts []string
	if c.LoopCount != nil {
		parts = appendCount(parts, c.LoopCount(), "loop", "loops")
	}
	if c.BackgroundCalls != nil {
		// "background" doubles as a count noun and an adjective, so the plural
		// needs the word: "3 background" reads as a truncated label.
		parts = appendCount(parts, c.BackgroundCalls(), "background call", "background calls")
	}
	if len(parts) == 0 || hiddenLineCount > 0 {
		return c.Editor.DefaultRenderBottomBorder(width, hiddenLineCount)
	}
	label := strings.Join(parts, " · ")
	labelWidth := tui.VisibleWidth(label)
	if width < labelWidth+5 {
		return c.Editor.DefaultRenderBottomBorder(width, hiddenLineCount)
	}
	return c.BorderColor("── ") + ActiveTheme().Fg("muted", label) +
		c.BorderColor(" "+strings.Repeat("─", width-labelWidth-4))
}

// appendCount adds "1 <one>" or "N <many>" to parts, nothing for zero.
func appendCount(parts []string, count int, one string, many string) []string {
	switch {
	case count == 1:
		return append(parts, "1 "+one)
	case count > 1:
		return append(parts, strconv.Itoa(count)+" "+many)
	}
	return parts
}

// renderTopBorder renders the top border with the embedded working status.
func (c *CustomEditor) renderTopBorder(width int, hiddenLineCount int) string {
	if !c.EmbedWorkingStatus || c.workingStatusIndicator == nil || width <= 0 {
		return c.Editor.DefaultRenderTopBorder(width, hiddenLineCount)
	}

	status := c.workingStatusIndicator.RenderInBorder(max(1, width-5))
	statusWidth := tui.VisibleWidth(status)
	if statusWidth == 0 {
		return c.Editor.DefaultRenderTopBorder(width, hiddenLineCount)
	}

	overflowLabel := ""
	overflowLabelWidth := 0
	if hiddenLineCount > 0 {
		overflowLabel = " ↑ " + strconv.Itoa(hiddenLineCount) + " more "
		overflowLabelWidth = tui.VisibleWidth(overflowLabel)
	}
	overflowStart := (width - overflowLabelWidth) / 2
	canFitOverflow := func() bool {
		return overflowLabel != "" && overflowLabelWidth+2 <= width &&
			overflowStart-(3+statusWidth+1) >= 1
	}

	if overflowLabel != "" && !canFitOverflow() {
		status = c.workingStatusIndicator.RenderSpinnerInBorder(width)
		statusWidth = tui.VisibleWidth(status)
	}

	if canFitOverflow() {
		leftBlockWidth := 3 + statusWidth + 1
		return c.BorderColor("── ") + status +
			c.BorderColor(" "+strings.Repeat("─", max(0, overflowStart-leftBlockWidth))+
				overflowLabel+strings.Repeat("─", max(0, width-overflowStart-overflowLabelWidth)))
	}

	if width >= statusWidth+5 {
		return c.BorderColor("── ") + status +
			c.BorderColor(" "+strings.Repeat("─", max(0, width-statusWidth-4)))
	}

	status = c.workingStatusIndicator.RenderSpinnerInBorder(width)
	statusWidth = tui.VisibleWidth(status)
	prefixWidth := min(3, max(0, width-statusWidth))
	return c.BorderColor(strings.Repeat("─", prefixWidth)) + status +
		c.BorderColor(strings.Repeat("─", max(0, width-prefixWidth-statusWidth)))
}
