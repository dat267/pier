package interactive

import (
	"fmt"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Port of showSettingsSelector from
// src/modes/interactive/interactive-mode.ts: the settings config assembly and
// its change callbacks.
//
// Divergences: the collaborators (session, theme controller, renderer, chat,
// selector refresh) are injected as small interfaces/function values (D90).

// SettingsSession is the session surface the settings selector needs.
type SettingsSession interface {
	AutoCompactionEnabled() bool
	Model() *ai.Model
	GetAvailableModels() []*ai.Model
	SteeringMode() coding.QueueMode
	FollowUpMode() coding.QueueMode
	SetAutoCompactionEnabled(enabled bool)
	SetSteeringMode(mode coding.QueueMode)
	SetFollowUpMode(mode coding.QueueMode)
	SetTransport(transport ai.Transport)
	SetCacheWarmingMode(mode coding.CacheWarmingMode)
	SetThinkingLevel(level ai.ThinkingLevel, options ...coding.ModelMutationOptions)
	IsStreaming() bool
	// SetGoalFeatureEnabled turns the builtin goal feature on or off (D496).
	// D497 covers the tool toggles and the guarded controller accessor.
	SetGoalFeatureEnabled(enabled bool)
	// SetLoopFeatureEnabled turns the /loop feature on or off (D537), removing
	// its tools and dropping pending jobs when disabled.
	SetLoopFeatureEnabled(enabled bool) bool
	// SetToolEnabled turns one registered tool on or off (D497).
	SetToolEnabled(name string, enabled bool) error
	// ToolNames lists the registered tools.
	ToolNames() []string
	// GetActiveToolNames lists the tools the agent may call.
	GetActiveToolNames() []string
}

// SettingsThemeController is the theme-controller surface.
type SettingsThemeController interface {
	GetThemeSelection() string
	GetTerminalTheme() TerminalTheme
	SetThemeSetting(theme string) error
	Preview(theme string)
}

// SettingsWiring builds and shows the settings selector.
type SettingsWiring struct {
	// LoopCount reports the scheduled /loop jobs, which turning the loop
	// feature off drops (D537).
	LoopCount func() int
	// HasGoal reports whether the session has a goal, which turning the goal
	// feature off keeps (D496).
	HasGoal         func() bool
	Slot            *SelectorSlot
	Settings        *coding.SettingsManager
	Session         SettingsSession
	ThemeController SettingsThemeController
	UI              tui.TUI

	// Chat is the transcript container (for the image/thinking updates).
	Chat *tui.Container
	// PendingMessages is the queued-messages container; the output-padding
	// change reaches its children too (upstream onOutputPadChange).
	PendingMessages *tui.Container
	// StreamingComponent is the in-progress assistant component.
	StreamingComponent *AssistantMessageComponent
	// DefaultEditor is the built-in editor (padding/autocomplete updates).
	DefaultEditor *CustomEditor
	// Editor is the active editor (may differ from DefaultEditor).
	Editor tui.Component
	// Renderer is the active renderer (fullscreen copy-on-select).
	Renderer tui.TUI

	// UpdateThinkingBlockVisibility re-renders the assistant messages.
	UpdateThinkingBlockVisibility func(hidden bool)
	// RebuildChatFromMessages re-renders the transcript.
	RebuildChatFromMessages func()
	// UpdateEditorBorderColor refreshes the editor border.
	UpdateEditorBorderColor func()
	// SetupAutocompleteProvider rebuilds the autocomplete provider.
	SetupAutocompleteProvider func()
	// SwitchTuiMode switches the renderer mode.
	SwitchTuiMode func(mode string) bool
	// ApplyFullscreenScrollbarSetting applies the scrollbar setting.
	ApplyFullscreenScrollbarSetting func()
	// ShowStatus reports a status line.
	ShowStatus func(message string)
	// RequestRender requests a render.
	RequestRender func()
	// SetHideThinkingBlock tracks the hidden-thinking state.
	HideThinkingBlock *bool
	// OutputPad tracks the output padding.
	OutputPad *int
	// UpdateStatusContainer clears the status container when idle.
	ClearStatusContainerIfIdle func()
	// WireGoalController rebinds the goal controller's UI hooks and idle-round
	// scheduler after the feature is re-enabled (D496).
	WireGoalController func()
}

func (w *SettingsWiring) showStatus(message string) {
	if w.ShowStatus != nil {
		w.ShowStatus(message)
	}
}

func (w *SettingsWiring) requestRender() {
	if w.RequestRender != nil {
		w.RequestRender()
	} else if w.UI != nil {
		w.UI.RequestRender(false)
	}
}

// BuildSettingsConfig assembles the settings snapshot.
func (w *SettingsWiring) BuildSettingsConfig() SettingsConfig {
	settings := w.Settings
	session := w.Session
	defaultModel := "not set"
	if provider := settings.GetDefaultProvider(); provider != nil {
		if modelID := settings.GetDefaultModel(); modelID != nil {
			defaultModel = *provider + "/" + *modelID
		}
	}
	httpIdleTimeout, _ := settings.GetHTTPIdleTimeoutMS()
	config := SettingsConfig{
		AutoCompact:            session.AutoCompactionEnabled(),
		DefaultModel:           defaultModel,
		CurrentModel:           session.Model(),
		AvailableDefaultModels: session.GetAvailableModels(),
		ShowImages:             settings.GetShowImages(),
		ImageWidthCells:        settings.GetImageWidthCells(),
		AutoResizeImages:       settings.GetImageAutoResize(),
		BlockImages:            settings.GetBlockImages(),
		EnableSkillCommands:    settings.GetEnableSkillCommands(),
		SteeringMode:           session.SteeringMode(),
		FollowUpMode:           session.FollowUpMode(),
		Transport:              settings.GetTransport(),
		HTTPIdleTimeoutMs:      httpIdleTimeout,
		CacheWarmingMode:       settings.GetCacheWarmingMode(),
		ThinkingLevel:          coding.DefaultThinkingLevel,
		AvailableThinkingLevels: []string{
			"off", "minimal", "low", "medium", "high", "xhigh", "max",
		},
		ModelThinkingLevels:        settings.GetAllModelThinkingLevels(),
		CurrentTheme:               "dark",
		TerminalTheme:              TerminalThemeDark,
		AvailableThemes:            AvailableThemes(),
		HideThinkingBlock:          settings.GetHideThinkingBlock(),
		MermaidRenderingMode:       settings.GetMermaidRenderingMode(),
		CollapseChangelog:          settings.GetCollapseChangelog(),
		EnableInstallTelemetry:     settings.GetEnableInstallTelemetry(),
		DoubleEscapeAction:         settings.GetDoubleEscapeAction(),
		TreeFilterMode:             settings.GetTreeFilterMode(),
		ShowHardwareCursor:         settings.GetShowHardwareCursor(),
		ShowCacheMissNotices:       settings.GetShowCacheMissNotices(),
		DefaultProjectTrust:        settings.GetDefaultProjectTrust(),
		EditorPaddingX:             settings.GetEditorPaddingX(),
		OutputPad:                  settings.GetOutputPad(),
		AutocompleteMaxVisible:     settings.GetAutocompleteMaxVisible(),
		QuietStartup:               settings.GetQuietStartup(),
		ClearOnShrink:              settings.GetClearOnShrink(),
		ShowTerminalProgress:       settings.GetShowTerminalProgress(),
		TuiMode:                    "regular",
		FullscreenExitOutput:       settings.GetFullscreenExitOutput(),
		FullscreenScrollbar:        settings.GetFullscreenScrollbar(),
		FullscreenCopyOnSelect:     settings.GetFullscreenCopyOnSelect(),
		FullscreenWheelScrollLines: settings.GetFullscreenWheelScrollLines(),
		Warnings:                   warningSettingsFromCoding(settings.GetWarnings()),
	}
	if value := settings.GetDefaultThinkingLevel(); value != nil && *value != "" {
		config.ThinkingLevel = *value
	}
	if w.ThemeController != nil {
		if selection := w.ThemeController.GetThemeSelection(); selection != "" {
			config.CurrentTheme = selection
		}
		config.TerminalTheme = w.ThemeController.GetTerminalTheme()
	}
	if w.UI != nil {
		config.TuiMode = TuiMode(w.UI)
	}
	return config
}

// TuiMode reports the renderer mode ("" for the regular renderer).
func TuiMode(ui tui.TUI) string {
	if _, ok := tuiConcrete(ui).(*tui.AltScreen); ok {
		return "fullscreen"
	}
	return "regular"
}

// tuiConcrete unwraps the UI forwarding reference so type assertions see the
// active renderer (upstream reads this.renderer where the port holds app.UI).
func tuiConcrete(ui tui.TUI) tui.TUI {
	if reference, ok := ui.(interface{ Current() tui.TUI }); ok {
		return reference.Current()
	}
	return ui
}

func warningSettingsFromCoding(warnings coding.SettingsWarnings) WarningSettings {
	return WarningSettings{AnthropicExtraUsage: warnings.AnthropicExtraUsage}
}

// BuildSettingsCallbacks assembles the change callbacks.
func (w *SettingsWiring) BuildSettingsCallbacks(done func(), refresh func()) SettingsCallbacks {
	settings := w.Settings
	session := w.Session
	callbacks := SettingsCallbacks{
		OnAutoCompactChange: func(enabled bool) {
			session.SetAutoCompactionEnabled(enabled)
		},
		OnShowImagesChange: func(enabled bool) {
			settings.SetShowImages(enabled)
			for _, child := range chatChildren(w.Chat) {
				if tool, ok := child.(*ToolExecutionComponent); ok {
					tool.SetShowImages(enabled)
				}
			}
		},
		OnImageWidthCellsChange: func(width int) {
			settings.SetImageWidthCells(width)
			for _, child := range chatChildren(w.Chat) {
				if tool, ok := child.(*ToolExecutionComponent); ok {
					tool.SetImageWidthCells(width)
				}
			}
		},
		OnAutoResizeImagesChange: func(enabled bool) { settings.SetImageAutoResize(enabled) },
		OnBlockImagesChange:      func(blocked bool) { settings.SetBlockImages(blocked) },
		OnEnableSkillCommandsChange: func(enabled bool) {
			settings.SetEnableSkillCommands(enabled)
			if w.SetupAutocompleteProvider != nil {
				w.SetupAutocompleteProvider()
			}
		},
		OnSteeringModeChange: func(mode string) { session.SetSteeringMode(mode) },
		OnFollowUpModeChange: func(mode string) { session.SetFollowUpMode(mode) },
		OnTransportChange: func(transport ai.Transport) {
			settings.SetTransport(transport)
			session.SetTransport(transport)
		},
		OnHTTPIdleTimeoutMsChange: func(timeoutMS int64) {
			// The value is written here and applied at the next start, where the
			// dispatcher is configured before any request exists (D254): Go's
			// transport has no safe way to be reconfigured while in flight.
			if err := settings.SetHTTPIdleTimeoutMS(timeoutMS); err != nil {
				w.showStatus("Could not save the HTTP idle timeout: " + err.Error())
				return
			}
			w.showStatus("HTTP idle timeout: " + coding.FormatHTTPIdleTimeoutMS(timeoutMS))
		},
		OnCacheWarmingModeChange: func(mode string) {
			session.SetCacheWarmingMode(mode)
			w.showStatus("Cache warming: " + mode)
		},
		OnModelThinkingLevelChange: func(provider string, modelID string, level string) {
			settings.SetModelThinkingLevel(provider, modelID, level)
			if current := session.Model(); current != nil && current.Provider == provider && current.ID == modelID {
				session.SetThinkingLevel(level)
				if w.UpdateEditorBorderColor != nil {
					w.UpdateEditorBorderColor()
				}
			}
		},
		OnModelThinkingLevelRemove: func(provider string, modelID string) {
			settings.RemoveModelThinkingLevel(provider, modelID)
			if current := session.Model(); current != nil && current.Provider == provider && current.ID == modelID {
				level := coding.DefaultThinkingLevel
				if value := settings.GetDefaultThinkingLevel(); value != nil && *value != "" {
					level = *value
				}
				session.SetThinkingLevel(level)
				if w.UpdateEditorBorderColor != nil {
					w.UpdateEditorBorderColor()
				}
			}
		},
		OnThemeChange: func(themeSetting string) {
			settings.SetTheme(themeSetting)
			if w.ThemeController != nil {
				_ = w.ThemeController.SetThemeSetting(themeSetting)
			}
		},
		OnThemePreview: func(themeName string) {
			if w.ThemeController != nil {
				w.ThemeController.Preview(themeName)
			}
		},
		OnHideThinkingBlockChange: func(hidden bool) {
			settings.SetHideThinkingBlock(hidden)
			w.applyThinkingBlockVisibility(hidden)
		},
		OnMermaidRenderingModeChange: func(mode string) {
			settings.SetMermaidRenderingMode(mode)
			if w.Chat != nil {
				w.Chat.Invalidate()
			}
			w.requestRender()
		},
		OnShowCacheMissNoticesChange: func(shown bool) {
			settings.SetShowCacheMissNotices(shown)
			if w.RebuildChatFromMessages != nil {
				w.RebuildChatFromMessages()
			}
		},
		OnCollapseChangelogChange:      func(collapsed bool) { settings.SetCollapseChangelog(collapsed) },
		OnEnableInstallTelemetryChange: func(enabled bool) { settings.SetEnableInstallTelemetry(enabled) },
		OnQuietStartupChange:           func(value coding.QuietStartupSetting) { settings.SetQuietStartup(value) },
		OnDefaultProjectTrustChange:    func(trust string) { settings.SetDefaultProjectTrust(trust) },
		OnDoubleEscapeActionChange:     func(action string) { settings.SetDoubleEscapeAction(action) },
		OnTreeFilterModeChange:         func(mode string) { settings.SetTreeFilterMode(mode) },
		OnShowHardwareCursorChange: func(enabled bool) {
			settings.SetShowHardwareCursor(enabled)
			w.applyShowHardwareCursor(enabled)
		},
		OnEditorPaddingXChange: func(padding int) {
			settings.SetEditorPaddingX(padding)
			w.applyEditorPaddingX(padding)
		},
		OnOutputPadChange: func(padding int) {
			settings.SetOutputPad(padding)
			w.applyOutputPad(padding)
		},
		OnAutocompleteMaxVisibleChange: func(maxVisible int) {
			settings.SetAutocompleteMaxVisible(maxVisible)
			w.applyAutocompleteMaxVisible(maxVisible)
		},
		OnClearOnShrinkChange: func(enabled bool) {
			settings.SetClearOnShrink(enabled)
			w.applyClearOnShrink(enabled)
		},
		OnShowTerminalProgressChange: func(enabled bool) { settings.SetShowTerminalProgress(enabled) },
		OnTuiModeChange: func(mode string) {
			if w.SwitchTuiMode != nil && !w.SwitchTuiMode(mode) {
				if refresh != nil {
					refresh()
				}
				w.showStatus("Close active overlays before changing TUI mode")
				return
			}
			settings.SetTuiMode(mode)
			if w.ClearStatusContainerIfIdle != nil {
				w.ClearStatusContainerIfIdle()
			}
			w.showStatus("TUI mode: " + mode)
		},
		OnFullscreenExitOutputChange: func(output string) { settings.SetFullscreenExitOutput(output) },
		OnFullscreenScrollbarChange: func(mode string) {
			settings.SetFullscreenScrollbar(mode)
			if w.ApplyFullscreenScrollbarSetting != nil {
				w.ApplyFullscreenScrollbarSetting()
			}
		},
		OnFullscreenCopyOnSelectChange: func(enabled bool) {
			settings.SetFullscreenCopyOnSelect(enabled)
			w.applyFullscreenCopyOnSelect(enabled)
		},
		OnFullscreenWheelScrollLinesChange: func(lines tui.WheelScrollLines) {
			settings.SetFullscreenWheelScrollLines(lines)
			w.applyFullscreenWheelScrollLines(lines)
		},
		OnWarningsChange: func(warnings WarningSettings) {
			settings.SetWarnings(coding.SettingsWarnings{AnthropicExtraUsage: warnings.AnthropicExtraUsage})
		},
		OnCancel: func() {
			if done != nil {
				done()
			}
			if refresh != nil {
				refresh()
			}
			w.requestRender()
		},
	}
	return callbacks
}

// ShowSettingsSelector opens the settings selector.
func (w *SettingsWiring) ShowSettingsSelector() {
	w.Slot.Show(func(done func()) CreatedSelector {
		var selector *SettingsSelectorComponent
		callbacks := w.BuildSettingsCallbacks(done, func() {
			// The TUI-mode rejection resets the row to the renderer's mode.
			if selector == nil {
				return
			}
			mode := "regular"
			if w.UI != nil {
				mode = TuiMode(w.UI)
			}
			selector.GetSettingsList().UpdateValue("tui-mode", mode)
		})
		selector = NewSettingsSelectorComponent(w.BuildSettingsConfig(), callbacks)
		return CreatedSelector{Component: selector, Focus: selector.GetSettingsList()}
	})
}

func chatChildren(chat *tui.Container) []tui.Component {
	if chat == nil {
		return nil
	}
	return chat.Children
}

// newSettingsWiring assembles the SettingsWiring (port of the corresponding InteractiveMode wiring).
func newSettingsWiring(app *App) *SettingsWiring {
	return &SettingsWiring{
		// Turning clear-on-shrink off drops a stale idle status line (upstream
		// clears the status container when no indicator is active).
		ClearStatusContainerIfIdle: func() {
			if app.uiState != nil {
				app.uiState.ClearStatusContainerIfIdle()
			}
		},
		// The scrollbar row's side effect: the transcript's scroll view follows the
		// setting, the same way a reload applies it.
		ApplyFullscreenScrollbarSetting: func() { app.applyFullscreenScrollbarSetting() },
		WireGoalController:              func() { app.wireGoalController() },
		HasGoal: func() bool {
			controller := app.session.GoalController()
			return controller != nil && controller.Snapshot().Goal != nil
		},
		LoopCount: func() int {
			if app.loopScheduler == nil {
				return 0
			}
			return app.loopScheduler.Count()
		},
		Slot:                          app.slot,
		Settings:                      app.settings,
		Session:                       app.session,
		ThemeController:               themeSettingsControllerAdapter{app.theme},
		UI:                            app.ui,
		Chat:                          app.chat,
		PendingMessages:               app.pendingMessages,
		DefaultEditor:                 app.defaultEditor,
		Editor:                        app.defaultEditor,
		Renderer:                      app.ui,
		UpdateThinkingBlockVisibility: func(hidden bool) { app.updateThinkingBlockVisibility(hidden) },
		// DisplayOptions is the single owner of these values; point the settings
		// callbacks at it so a change reaches the transcript without a /reload.
		// The output-padding callback rebuilt the chat with the stale padding
		// (the transcript renderer reads Display.OutputPad).
		HideThinkingBlock:         &app.display.HideThinkingBlock,
		OutputPad:                 &app.display.OutputPad,
		RebuildChatFromMessages:   func() { app.startup.RebuildChatFromMessages() },
		UpdateEditorBorderColor:   func() { app.updateEditorBorderColor() },
		SetupAutocompleteProvider: func() { app.autocomplete.SetupAutocompleteProvider() },
		SwitchTuiMode:             func(mode string) bool { return app.lifecycle.SwitchTuiMode(mode, true, true) },
		ShowStatus:                func(message string) { app.transcript.ShowStatus(message) },
		RequestRender:             func() { app.ui.RequestRender(false) },
	}
}

// The apply* methods are the single owner-loop application of one settings
// value. The /settings callbacks persist then apply; applySettingsDependentUI
// applies without persisting, so a /reload or session switch never writes the
// setting back and both paths reach the same final state.

func (w *SettingsWiring) applyThinkingBlockVisibility(hidden bool) {
	if w.HideThinkingBlock != nil {
		*w.HideThinkingBlock = hidden
	}
	if w.UpdateThinkingBlockVisibility != nil {
		w.UpdateThinkingBlockVisibility(hidden)
	}
}

func (w *SettingsWiring) applyShowHardwareCursor(enabled bool) {
	if w.UI != nil {
		w.UI.SetShowHardwareCursor(enabled)
	}
}

func (w *SettingsWiring) applyEditorPaddingX(padding int) {
	if w.DefaultEditor != nil {
		w.DefaultEditor.SetPaddingX(padding)
	}
	if w.Editor != nil && w.Editor != tui.Component(w.DefaultEditor) {
		if editor, ok := w.Editor.(interface{ SetPaddingX(int) }); ok {
			editor.SetPaddingX(padding)
		}
	}
}

func (w *SettingsWiring) applyAutocompleteMaxVisible(maxVisible int) {
	if w.DefaultEditor != nil {
		w.DefaultEditor.SetAutocompleteMaxVisible(maxVisible)
	}
	if w.Editor != nil && w.Editor != tui.Component(w.DefaultEditor) {
		if editor, ok := w.Editor.(interface{ SetAutocompleteMaxVisible(int) }); ok {
			editor.SetAutocompleteMaxVisible(maxVisible)
		}
	}
}

func (w *SettingsWiring) applyClearOnShrink(enabled bool) {
	if w.UI != nil {
		w.UI.SetClearOnShrink(enabled)
	}
	if !enabled && w.ClearStatusContainerIfIdle != nil {
		w.ClearStatusContainerIfIdle()
	}
}

func (w *SettingsWiring) applyFullscreenCopyOnSelect(enabled bool) {
	if renderer, ok := tuiConcrete(w.Renderer).(*tui.AltScreen); ok {
		renderer.SetCopyOnSelect(enabled)
	}
}

func (w *SettingsWiring) applyFullscreenWheelScrollLines(lines tui.WheelScrollLines) {
	if renderer, ok := tuiConcrete(w.Renderer).(*tui.AltScreen); ok {
		renderer.SetWheelScrollLines(lines)
	}
}

// applyOutputPad installs the padding and refreshes the rendered messages: an
// active stream's components update in place, otherwise the transcript
// rebuilds with the new padding. It returns whether a rebuild was performed,
// letting reload reuse that work instead of rebuilding the transcript twice.
func (w *SettingsWiring) applyOutputPad(padding int) bool {
	if w.OutputPad != nil {
		*w.OutputPad = padding
	}
	streaming := w.StreamingComponent != nil || (w.Session != nil && w.Session.IsStreaming())
	if streaming {
		// Upstream iterates both the transcript and the pending-messages
		// container, so a queued bash block follows the padding too.
		for _, container := range []*tui.Container{w.Chat, w.PendingMessages} {
			for _, child := range chatChildren(container) {
				switch component := child.(type) {
				case *AssistantMessageComponent:
					component.SetOutputPad(padding)
				case *CustomMessageComponent:
					component.SetOutputPad(padding)
				case *UserMessageComponent:
					component.SetOutputPad(padding)
				case *ToolExecutionComponent:
					// Tool output is padded like the rest of the transcript (upstream #10557).
					component.SetOutputPad(padding)
				case *BranchSummaryMessageComponent:
					component.SetOutputPad(padding)
				case *CompactionSummaryMessageComponent:
					component.SetOutputPad(padding)
				}
			}
		}
		if w.StreamingComponent != nil {
			w.StreamingComponent.SetOutputPad(padding)
		}
		w.requestRender()
		return false
	}
	if w.RebuildChatFromMessages != nil {
		w.RebuildChatFromMessages()
		return true
	}
	return false
}

// featureToggles lists the pier-only feature toggles for /features (D496). It is
// the only place a new pier-only feature is declared.
func (w *SettingsWiring) featureToggles() []FeatureToggle {
	return []FeatureToggle{
		{
			ID:          "goal-feature",
			Label:       "Goal",
			Description: "Enable /goal and the get_goal and set_goal tools",
			Enabled:     w.Settings.GetGoalFeatureEnabled(),
		},
		{
			ID:          "loop-feature",
			Label:       "Loop",
			Description: "Enable /loop and the scheduling tools",
			Enabled:     w.Settings.GetLoopFeatureEnabled(),
		},
		{
			ID:    "background-tools",
			Label: "Background tools",
			Description: "Move a tool call still running after this long to the background, " +
				"so the turn stays responsive; the model is told once when it ends",
			Values:       coding.BackgroundToolCallChoices(),
			CurrentValue: backgroundToolCallChoice(w.Settings.GetBackgroundToolCallThreshold()),
		},
	}
}

// ShowFeatureSettingsSelector opens the /features selector: the pier-only feature
// toggles, kept out of /settings so that list stays the reference's (D496).
func (w *SettingsWiring) ShowFeatureSettingsSelector() {
	w.Slot.Show(func(done func()) CreatedSelector {
		selector := NewFeatureSettingsSelector(FeatureSelectorOptions{
			Toggles:         w.featureToggles(),
			OnFeatureChange: w.HandleFeatureChange,
			OnCancel:        done,
		})
		return CreatedSelector{Component: selector, Focus: selector.GetSettingsList()}
	})
}

// ShowToolsSelector opens the /tools selector: every registered tool with its
// active state (D496), so the agent's tool set can be narrowed without editing
// defaultTools and without a restart.
func (w *SettingsWiring) ShowToolsSelector() {
	w.Slot.Show(func(done func()) CreatedSelector {
		selector := NewToolSettingsSelector(w.toolToggles(), func(name string, enabled bool) {
			if err := w.Session.SetToolEnabled(name, enabled); err != nil {
				w.showStatus(err.Error())
				return
			}
			w.showStatus(toolToggleStatus(name, enabled))
		}, done)
		return CreatedSelector{Component: selector, Focus: selector.GetSettingsList()}
	})
}

// toolToggles lists the session's registered tools with their active state.
func (w *SettingsWiring) toolToggles() []ToolToggle {
	active := map[string]bool{}
	for _, name := range w.Session.GetActiveToolNames() {
		active[name] = true
	}
	names := w.Session.ToolNames()
	rows := make([]ToolToggle, 0, len(names))
	for _, name := range names {
		rows = append(rows, ToolToggle{Name: name, Label: name, Enabled: active[name]})
	}
	return rows
}

// toolToggleStatus names the new state of a tool toggle.
func toolToggleStatus(name string, enabled bool) string {
	if enabled {
		return "Tool " + name + " enabled."
	}
	return "Tool " + name + " disabled."
}

// HandleFeatureChange applies one /features row and reports the new state. The
// loop row's "off" also names the loops it stopped (D537): they are dropped with
// the feature, and nothing else would say so.
func (w *SettingsWiring) HandleFeatureChange(id string, value string) {
	stopped := 0
	if id == "loop-feature" && value != "true" && w.LoopCount != nil {
		stopped = w.LoopCount()
	}
	keptGoal := id == "goal-feature" && value != "true" && w.HasGoal != nil && w.HasGoal()
	if !w.ApplyFeatureToggle(id, value) {
		return
	}
	status := featureToggleStatus(id, value)
	switch {
	case stopped == 1:
		status = strings.TrimSuffix(status, ".") + "; stopped 1 loop."
	case stopped > 1:
		status = strings.TrimSuffix(status, ".") + fmt.Sprintf("; stopped %d loops.", stopped)
	case keptGoal:
		status = strings.TrimSuffix(status, ".") + "; the goal is kept for when it is back on."
	}
	w.showStatus(status)
}

// ApplyFeatureToggle stores one pier-only feature toggle and applies it to the
// live session; it reports whether the id is a known feature.
func (w *SettingsWiring) ApplyFeatureToggle(id string, value string) bool {
	enabled := value == "true"
	switch id {
	case "loop-feature":
		w.Settings.SetLoopFeatureEnabled(enabled)
		w.Session.SetLoopFeatureEnabled(enabled)
		// Apply it to the live session: off removes the scheduling tools, drops
		// pending jobs and makes /loop report that it is unavailable.
		return true
	case "goal-feature":
		w.Settings.SetGoalFeatureEnabled(enabled)
		w.Session.SetGoalFeatureEnabled(enabled)
		if w.WireGoalController != nil {
			w.WireGoalController()
		}
		return true
	case "background-tools":
		// Stored; the session reads it per run, so the next tool call obeys.
		return w.Settings.SetBackgroundToolCallThreshold(value)
	}
	return false
}

// featureToggleStatus names the new state of a feature toggle.
func featureToggleStatus(id string, value string) string {
	state := "off"
	if value == "true" {
		state = "on"
	}
	switch id {
	case "goal-feature":
		return "Goal " + state + "."
	case "loop-feature":
		return "Loop " + state + "."
	case "background-tools":
		if value == "off" {
			return "Automatic backgrounding off."
		}
		return "Long tool calls moved to the background after " + value + "."
	}
	return state
}

// backgroundToolCallChoice names the choice row's current value. It has to be one
// of the row's own values: the settings list cycles from the index of the current
// value, so a value outside the list makes Enter jump to the first entry instead
// of moving to the next one.
func backgroundToolCallChoice(threshold time.Duration) string {
	if threshold <= 0 {
		return "off"
	}
	// The row's spelling: Duration.String() gives "1m0s" and "2m0s", which are
	// not among the choices, and an unlisted value makes Enter jump to the first
	// entry instead of the next one. A stored threshold between the offered steps
	// still shows itself ("1m30s"), so the row never misreports what the harness
	// will do.
	text := threshold.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}
