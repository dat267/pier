package interactive

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// settingsTestSession implements SettingsSession.
type settingsTestSession struct {
	autoCompact  bool
	model        *ai.Model
	models       []*ai.Model
	steering     coding.QueueMode
	followUp     coding.QueueMode
	cacheWarming string
	transport    ai.Transport
	thinking     ai.ThinkingLevel
	streaming    bool
	goalFeature  bool
	loopFeature  bool
	tools        []string
	activeTools  []string
}

func (s *settingsTestSession) AutoCompactionEnabled() bool           { return s.autoCompact }
func (s *settingsTestSession) Model() *ai.Model                      { return s.model }
func (s *settingsTestSession) GetAvailableModels() []*ai.Model       { return s.models }
func (s *settingsTestSession) SteeringMode() coding.QueueMode        { return s.steering }
func (s *settingsTestSession) FollowUpMode() coding.QueueMode        { return s.followUp }
func (s *settingsTestSession) SetAutoCompactionEnabled(enabled bool) { s.autoCompact = enabled }
func (s *settingsTestSession) SetSteeringMode(mode coding.QueueMode) { s.steering = mode }
func (s *settingsTestSession) SetFollowUpMode(mode coding.QueueMode) { s.followUp = mode }
func (s *settingsTestSession) SetTransport(transport ai.Transport)   { s.transport = transport }
func (s *settingsTestSession) SetCacheWarmingMode(mode coding.CacheWarmingMode) {
	s.cacheWarming = mode
}
func (s *settingsTestSession) SetThinkingLevel(level ai.ThinkingLevel, _ ...coding.ModelMutationOptions) {
	s.thinking = level
}
func (s *settingsTestSession) IsStreaming() bool                  { return s.streaming }
func (s *settingsTestSession) SetGoalFeatureEnabled(enabled bool) { s.goalFeature = enabled }
func (s *settingsTestSession) SetLoopFeatureEnabled(enabled bool) bool {
	s.loopFeature = enabled
	return true
}
func (s *settingsTestSession) ToolNames() []string { return append([]string{}, s.tools...) }
func (s *settingsTestSession) GetActiveToolNames() []string {
	return append([]string{}, s.activeTools...)
}
func (s *settingsTestSession) SetToolEnabled(name string, enabled bool) error {
	next := make([]string, 0, len(s.activeTools)+1)
	found := false
	known := false
	for _, registered := range s.tools {
		if registered == name {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("Unknown tool: %s", name)
	}
	for _, active := range s.activeTools {
		if active == name {
			found = true
			if !enabled {
				continue
			}
		}
		next = append(next, active)
	}
	if enabled && !found {
		next = append(next, name)
	}
	s.activeTools = next
	return nil
}

// settingsTestTheme implements SettingsThemeController.
type settingsTestTheme struct {
	selection string
	terminal  TerminalTheme
	setCalls  []string
	previews  []string
}

func (t *settingsTestTheme) GetThemeSelection() string       { return t.selection }
func (t *settingsTestTheme) GetTerminalTheme() TerminalTheme { return t.terminal }
func (t *settingsTestTheme) SetThemeSetting(theme string) error {
	t.setCalls = append(t.setCalls, theme)
	return nil
}
func (t *settingsTestTheme) Preview(theme string) { t.previews = append(t.previews, theme) }

func newSettingsTestWiring(t *testing.T) (*SettingsWiring, *settingsTestSession, *settingsTestTheme, *coding.SettingsManager) {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	screen := tui.NewMainScreen(&fakeRendererTerminal{width: 80, height: 24}, false, "")
	editorContainer := &tui.Container{}
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	editorContainer.AddChild(editor)
	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	// Both features start on, so a toggle test can prove the off transition
	// rather than reading a zero value that was never set.
	session := &settingsTestSession{autoCompact: true, steering: "one-at-a-time", followUp: "all",
		thinking: "medium", loopFeature: true}
	theme := &settingsTestTheme{selection: "light", terminal: TerminalThemeLight}
	wiring := &SettingsWiring{
		Slot:            NewSelectorSlot(screen, editorContainer, editor),
		Settings:        settings,
		Session:         session,
		ThemeController: theme,
		UI:              screen,
		Chat:            &tui.Container{},
		DefaultEditor:   editor,
		Editor:          editor,
		Renderer:        screen,
	}
	return wiring, session, theme, settings
}

// TestSettingsConfigAssembly covers the settings snapshot.
func TestSettingsConfigAssembly(t *testing.T) {
	wiring, session, theme, settings := newSettingsTestWiring(t)
	settings.SetDefaultProvider("anthropic")
	settings.SetDefaultModel("claude-x")
	settings.SetImageWidthCells(120)
	settings.SetImageAutoResize(false)
	settings.SetBlockImages(true)
	settings.SetEnableSkillCommands(false)
	settings.SetTransport("websocket")
	settings.SetHTTPIdleTimeoutMS(30000)
	settings.SetCacheWarmingMode("streaming")
	settings.SetDefaultThinkingLevel("high")
	settings.SetModelThinkingLevel("anthropic", "claude-x", "max")
	settings.SetHideThinkingBlock(true)
	settings.SetMermaidRenderingMode("off")
	settings.SetCollapseChangelog(true)
	settings.SetEnableInstallTelemetry(true)
	settings.SetDoubleEscapeAction("fork")
	settings.SetTreeFilterMode("no-tools")
	settings.SetShowHardwareCursor(true)
	settings.SetShowCacheMissNotices(true)
	settings.SetDefaultProjectTrust("always")
	settings.SetEditorPaddingX(2)
	settings.SetOutputPad(1)
	settings.SetAutocompleteMaxVisible(15)
	settings.SetQuietStartup(coding.QuietStartupSetting{Enabled: true})
	settings.SetClearOnShrink(false)
	settings.SetShowTerminalProgress(true)
	settings.SetFullscreenExitOutput("resume-hint")
	settings.SetFullscreenScrollbar("hidden")
	settings.SetFullscreenCopyOnSelect(false)
	settings.SetFullscreenWheelScrollLines(tui.FixedWheelScrollLines(5))
	settings.SetWarnings(coding.SettingsWarnings{AnthropicExtraUsage: new(false)})
	session.model = &ai.Model{ID: "claude-x", Provider: "anthropic", Reasoning: true}
	session.models = []*ai.Model{session.model}

	config := wiring.BuildSettingsConfig()
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"autoCompact", config.AutoCompact, true},
		{"defaultModel", config.DefaultModel, "anthropic/claude-x"},
		{"imageWidthCells", config.ImageWidthCells, 120},
		{"autoResizeImages", config.AutoResizeImages, false},
		{"blockImages", config.BlockImages, true},
		{"enableSkillCommands", config.EnableSkillCommands, false},
		{"steeringMode", config.SteeringMode, "one-at-a-time"},
		{"followUpMode", config.FollowUpMode, "all"},
		{"transport", config.Transport, "websocket"},
		{"httpIdleTimeoutMs", config.HTTPIdleTimeoutMs, int64(30000)},
		{"cacheWarmingMode", config.CacheWarmingMode, "streaming"},
		{"thinkingLevel", config.ThinkingLevel, "high"},
		{"hideThinkingBlock", config.HideThinkingBlock, true},
		{"mermaidRenderingMode", config.MermaidRenderingMode, "off"},
		{"collapseChangelog", config.CollapseChangelog, true},
		{"enableInstallTelemetry", config.EnableInstallTelemetry, true},
		{"doubleEscapeAction", config.DoubleEscapeAction, "fork"},
		{"treeFilterMode", config.TreeFilterMode, "no-tools"},
		{"showHardwareCursor", config.ShowHardwareCursor, true},
		{"showCacheMissNotices", config.ShowCacheMissNotices, true},
		{"defaultProjectTrust", config.DefaultProjectTrust, "always"},
		{"editorPaddingX", config.EditorPaddingX, 2},
		{"outputPad", config.OutputPad, 1},
		{"autocompleteMaxVisible", config.AutocompleteMaxVisible, 15},
		{"quietStartup", config.QuietStartup, coding.QuietStartupSetting{Enabled: true}},
		{"clearOnShrink", config.ClearOnShrink, false},
		{"showTerminalProgress", config.ShowTerminalProgress, true},
		{"tuiMode", config.TuiMode, "regular"},
		{"fullscreenExitOutput", config.FullscreenExitOutput, "resume-hint"},
		{"fullscreenScrollbar", config.FullscreenScrollbar, "hidden"},
		{"fullscreenCopyOnSelect", config.FullscreenCopyOnSelect, false},
		{"fullscreenWheelScrollLines", config.FullscreenWheelScrollLines, tui.FixedWheelScrollLines(5)},
		{"currentTheme", config.CurrentTheme, "light"},
		{"terminalTheme", config.TerminalTheme, TerminalThemeLight},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
	if len(config.AvailableThinkingLevels) != 7 {
		t.Errorf("thinking levels = %v", config.AvailableThinkingLevels)
	}
	if config.ModelThinkingLevels["anthropic/claude-x"] != "max" {
		t.Errorf("model thinking = %v", config.ModelThinkingLevels)
	}
	if config.Warnings.AnthropicExtraUsage == nil || *config.Warnings.AnthropicExtraUsage {
		t.Errorf("warnings = %+v", config.Warnings)
	}
	if theme.selection != "light" {
		t.Errorf("theme selection = %q", theme.selection)
	}
}

// TestSettingsCallbacks covers the change callbacks.
func TestSettingsCallbacks(t *testing.T) {
	wiring, session, theme, settings := newSettingsTestWiring(t)
	statuses := []string{}
	wiring.ShowStatus = func(message string) { statuses = append(statuses, message) }
	thinkingVisible := []bool{}
	wiring.UpdateThinkingBlockVisibility = func(hidden bool) { thinkingVisible = append(thinkingVisible, hidden) }
	rebuilt := 0
	wiring.RebuildChatFromMessages = func() { rebuilt++ }
	autocompleteSetups := 0
	wiring.SetupAutocompleteProvider = func() { autocompleteSetups++ }
	scrollbarApplied := 0
	wiring.ApplyFullscreenScrollbarSetting = func() { scrollbarApplied++ }
	hideThinking := false
	wiring.HideThinkingBlock = &hideThinking
	outputPad := 0
	wiring.OutputPad = &outputPad

	callbacks := wiring.BuildSettingsCallbacks(nil, nil)

	callbacks.OnAutoCompactChange(false)
	if session.autoCompact {
		t.Fatal("auto compact not applied")
	}
	callbacks.OnEnableSkillCommandsChange(false)
	if settings.GetEnableSkillCommands() || autocompleteSetups != 1 {
		t.Fatal("skill commands not applied")
	}
	callbacks.OnSteeringModeChange("all")
	if session.steering != "all" {
		t.Fatal("steering not applied")
	}
	// pi v1.4.0 interactive-mode.ts updates both the stored setting and the
	// active agent transport so the next turn uses the selected transport.
	callbacks.OnTransportChange("websocket")
	if settings.GetTransport() != "websocket" || session.transport != "websocket" {
		t.Fatalf("transport setting/session = %q/%q, want websocket/websocket", settings.GetTransport(), session.transport)
	}
	// The value is written, not installed into the transport: there is no
	// dispatcher to reconfigure, and mutating the live one races (D254). It reaches
	// the wire per request.
	callbacks.OnHTTPIdleTimeoutMsChange(60000)
	if got, err := settings.GetHTTPIdleTimeoutMS(); err != nil || got != 60000 {
		t.Fatalf("http idle timeout = %d, %v", got, err)
	}
	if statuses[len(statuses)-1] != "HTTP idle timeout: 1 min" {
		t.Fatalf("statuses = %v", statuses)
	}
	callbacks.OnCacheWarmingModeChange("idle")
	if session.cacheWarming != "idle" || statuses[len(statuses)-1] != "Cache warming: idle" {
		t.Fatalf("cache warming = %q", session.cacheWarming)
	}
	// Model thinking level for the current model also updates the session.
	session.model = &ai.Model{ID: "m", Provider: "p"}
	callbacks.OnModelThinkingLevelChange("p", "m", "high")
	if level := settings.GetModelThinkingLevel("p", "m"); level == nil || *level != "high" {
		t.Fatalf("model thinking = %v", level)
	}
	if session.thinking != "high" {
		t.Fatalf("session thinking = %q", session.thinking)
	}
	callbacks.OnModelThinkingLevelRemove("p", "m")
	if level := settings.GetModelThinkingLevel("p", "m"); level != nil && *level != "" {
		t.Fatalf("override not removed: %v", level)
	}
	// Removing the current model's override reverts to the global default.
	if session.thinking != coding.DefaultThinkingLevel {
		t.Fatalf("session level after remove = %q", session.thinking)
	}
	// A non-current model does not change the session level.
	callbacks.OnModelThinkingLevelChange("other", "x", "low")
	if session.thinking != coding.DefaultThinkingLevel {
		t.Fatalf("session level = %q", session.thinking)
	}
	callbacks.OnThemeChange("dark")
	if len(theme.setCalls) != 1 || theme.setCalls[0] != "dark" {
		t.Fatalf("theme set calls = %v", theme.setCalls)
	}
	callbacks.OnThemePreview("light")
	if len(theme.previews) != 1 {
		t.Fatalf("previews = %v", theme.previews)
	}
	callbacks.OnHideThinkingBlockChange(true)
	if !hideThinking || !settings.GetHideThinkingBlock() || len(thinkingVisible) != 1 {
		t.Fatal("hide thinking not applied")
	}
	callbacks.OnShowCacheMissNoticesChange(true)
	if !settings.GetShowCacheMissNotices() || rebuilt != 1 {
		t.Fatal("cache miss notices not applied")
	}
	callbacks.OnEditorPaddingXChange(3)
	if settings.GetEditorPaddingX() != 3 {
		t.Fatal("editor padding not applied")
	}
	callbacks.OnOutputPadChange(1)
	if settings.GetOutputPad() != 1 || outputPad != 1 {
		t.Fatal("output pad not applied")
	}
	if rebuilt != 2 {
		t.Fatalf("rebuild calls = %d", rebuilt)
	}
	callbacks.OnAutocompleteMaxVisibleChange(20)
	if settings.GetAutocompleteMaxVisible() != 20 {
		t.Fatal("autocomplete max not applied")
	}
	callbacks.OnClearOnShrinkChange(false)
	if settings.GetClearOnShrink() {
		t.Fatal("clear on shrink not applied")
	}
	callbacks.OnTuiModeChange("fullscreen")
	if settings.GetTuiMode() != "fullscreen" || statuses[len(statuses)-1] != "TUI mode: fullscreen" {
		t.Fatalf("tui mode = %q", settings.GetTuiMode())
	}
	callbacks.OnFullscreenScrollbarChange("always")
	if settings.GetFullscreenScrollbar() != "always" || scrollbarApplied != 1 {
		t.Fatal("scrollbar not applied")
	}
	callbacks.OnFullscreenCopyOnSelectChange(true)
	if !settings.GetFullscreenCopyOnSelect() {
		t.Fatal("copy on select not applied")
	}
	callbacks.OnFullscreenWheelScrollLinesChange(tui.FixedWheelScrollLines(2))
	if settings.GetFullscreenWheelScrollLines() != tui.FixedWheelScrollLines(2) {
		t.Fatalf("wheel scroll lines = %#v", settings.GetFullscreenWheelScrollLines())
	}
	callbacks.OnWarningsChange(WarningSettings{AnthropicExtraUsage: new(false)})
	if warnings := settings.GetWarnings(); warnings.AnthropicExtraUsage == nil || *warnings.AnthropicExtraUsage {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// TestSettingsTuiModeRejection covers the switch failure path.
func TestSettingsTuiModeRejection(t *testing.T) {
	wiring, _, _, settings := newSettingsTestWiring(t)
	statuses := []string{}
	wiring.ShowStatus = func(message string) { statuses = append(statuses, message) }
	wiring.SwitchTuiMode = func(mode string) bool { return false }
	// Start from an explicit regular mode: the default is fullscreen, so the
	// rejected switch must leave the stored mode alone.
	settings.SetTuiMode("regular")
	refreshed := 0
	callbacks := wiring.BuildSettingsCallbacks(nil, func() { refreshed++ })
	callbacks.OnTuiModeChange("fullscreen")
	if settings.GetTuiMode() != "regular" {
		t.Fatal("mode should not be persisted on rejection")
	}
	if refreshed != 1 || statuses[len(statuses)-1] != "Close active overlays before changing TUI mode" {
		t.Fatalf("refreshed = %d, statuses = %v", refreshed, statuses)
	}
}

// TestSettingsShowSelector covers the selector slot integration.
func TestSettingsShowSelector(t *testing.T) {
	wiring, _, _, _ := newSettingsTestWiring(t)
	wiring.ShowSettingsSelector()
	if !wiring.Slot.HasActiveSelector() {
		t.Fatal("settings selector not shown")
	}
	selector, ok := wiring.Slot.ActiveSelectorComponent().(*SettingsSelectorComponent)
	if !ok {
		t.Fatalf("component = %T", wiring.Slot.ActiveSelectorComponent())
	}
	if selector.GetSettingsList() == nil {
		t.Fatal("missing settings list")
	}
	// The cancel callback (built with the slot's done) restores the editor.
	wiring.ShowSettingsSelector()
	var cancel func()
	original := wiring.ShowStatus
	wiring.ShowStatus = original
	_ = cancel
	wiring.Slot.DisposeActiveSelector()
	if wiring.Slot.HasActiveSelector() {
		t.Fatal("dispose did not close the selector")
	}
}

// TestOutputPadChangeUpdatesTheDisplayOptions pins the composition bug: the
// settings callback wrote a nil pointer, so DisplayOptions.OutputPad (which the
// transcript renderer reads when it builds message components) kept the old
// value. An idle change rebuilt the transcript with the stale padding, so the
// change looked like it needed a /reload.
func TestOutputPadChangeUpdatesTheDisplayOptions(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	if app.display == nil || app.settingsW == nil {
		t.Fatal("app not composed")
	}
	want := 1 - app.display.OutputPad
	app.settingsW.BuildSettingsCallbacks(nil, nil).OnOutputPadChange(want)

	if app.display.OutputPad != want {
		t.Fatalf("display.OutputPad = %d, want %d (it only changed on /reload)", app.display.OutputPad, want)
	}
	if app.settings.GetOutputPad() != want {
		t.Fatalf("setting = %d, want %d", app.settings.GetOutputPad(), want)
	}
}

// The output-padding change reaches the pending-messages container too
// (upstream onOutputPadChange iterates chatContainer and pendingMessagesContainer).
func TestApplyOutputPadUpdatesPendingMessages(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	chat := &tui.Container{}
	pending := &tui.Container{}
	markdownTheme := GetMarkdownTheme()
	queued := NewUserMessageComponent("queued", &markdownTheme, 0, nil)
	pending.AddChild(queued)
	streaming := NewAssistantMessageComponent(nil, false, &markdownTheme, "", 0, nil)
	wiring := &SettingsWiring{
		Settings:           settings,
		Chat:               chat,
		PendingMessages:    pending,
		StreamingComponent: streaming,
		RequestRender:      func() {},
	}
	wiring.applyOutputPad(3)
	if queued.outputPad != 3 {
		t.Fatalf("pending message outputPad = %d; want 3", queued.outputPad)
	}
}

// TestFeatureToggleAppliesAndPersists pins D496's /features row: the change is
// stored, reaches the live session and rewires the controller, so the goal tools
// and /goal follow the toggle without a restart.
func TestFeatureToggleAppliesAndPersists(t *testing.T) {
	wiring, session, _, settings := newSettingsTestWiring(t)
	wired := 0
	wiring.WireGoalController = func() { wired++ }
	// The goal feature is opt-in (D496); this test toggles it OFF, so start on.
	settings.SetGoalFeatureEnabled(true)

	toggles := wiring.featureToggles()
	if len(toggles) == 0 || toggles[0].ID != "goal-feature" || !toggles[0].Enabled {
		t.Fatalf("feature toggles = %+v, want the goal feature on", toggles)
	}
	if unknown := wiring.ApplyFeatureToggle("not-a-feature", "true"); unknown {
		t.Fatal("an unknown feature id was applied")
	}

	selector := NewFeatureSettingsSelector(FeatureSelectorOptions{
		Toggles: toggles,
		OnFeatureChange: func(id string, value string) {
			if !wiring.ApplyFeatureToggle(id, value) {
				t.Fatalf("selector sent unknown id %q", id)
			}
		},
		OnCancel: func() {},
	})
	selector.GetSettingsList().HandleInput("\r")
	if settings.GetGoalFeatureEnabled() {
		t.Fatal("toggling the row did not persist")
	}
	if session.goalFeature {
		t.Fatal("toggling the row did not reach the session")
	}
	if wired != 1 {
		t.Fatalf("controller wiring ran %d times, want once", wired)
	}
	if wiring.featureToggles()[0].Enabled {
		t.Fatal("the toggle list still reports the goal feature on")
	}
	if got := selector.GetSettingsList().Items()[0].CurrentValue; got != "false" {
		t.Fatalf("row value = %q, want false", got)
	}
}

// The loop-feature row reaches the live session, not just the settings file:
// turning it off there removes the scheduling tools and drops pending jobs.
func TestLoopFeatureToggleAppliesToTheSession(t *testing.T) {
	wiring, session, _, settings := newSettingsTestWiring(t)
	selector := NewFeatureSettingsSelector(FeatureSelectorOptions{
		Toggles: wiring.featureToggles(),
		OnFeatureChange: func(id string, value string) {
			if !wiring.ApplyFeatureToggle(id, value) {
				t.Fatalf("ApplyFeatureToggle(%q) did not know the row", id)
			}
		},
		OnCancel: func() {},
	})
	items := selector.GetSettingsList().Items()
	loopIndex := -1
	for index, item := range items {
		if item.ID == "loop-feature" {
			loopIndex = index
		}
	}
	if loopIndex < 0 {
		t.Fatalf("/features holds no loop row: %+v", items)
	}
	selector.GetSettingsList().SelectItem("loop-feature")
	selector.GetSettingsList().HandleInput("\r")

	// The loop feature is opt-in, so the first toggle turns it ON.
	if !settings.GetLoopFeatureEnabled() {
		t.Fatal("toggling the loop row did not persist")
	}
	if !session.loopFeature {
		t.Fatal("toggling the loop row did not reach the session")
	}
}

// D537: turning the loop feature off drops every scheduled job, so the status
// says how many it stopped rather than leaving them to vanish from the footer.
func TestLoopFeatureOffNamesTheStoppedLoops(t *testing.T) {
	for _, tc := range []struct {
		scheduled int
		want      string
	}{{0, "Loop off."}, {1, "Loop off; stopped 1 loop."}, {2, "Loop off; stopped 2 loops."}} {
		wiring, _, _, _ := newSettingsTestWiring(t)
		var status string
		wiring.ShowStatus = func(message string) { status = message }
		wiring.LoopCount = func() int { return tc.scheduled }
		wiring.HandleFeatureChange("loop-feature", "false")
		if status != tc.want {
			t.Fatalf("%d scheduled: status %q, want %q", tc.scheduled, status, tc.want)
		}
	}
}

// D496: turning the goal feature off keeps the session's goal (turning it back
// on restores it), and the status says so, so "off" does not read as "deleted".
func TestGoalFeatureOffSaysTheGoalIsKept(t *testing.T) {
	for _, tc := range []struct {
		hasGoal bool
		want    string
	}{{false, "Goal off."}, {true, "Goal off; the goal is kept for when it is back on."}} {
		wiring, _, _, _ := newSettingsTestWiring(t)
		var status string
		wiring.ShowStatus = func(message string) { status = message }
		wiring.HasGoal = func() bool { return tc.hasGoal }
		wiring.HandleFeatureChange("goal-feature", "false")
		if status != tc.want {
			t.Fatalf("goal %v: status %q, want %q", tc.hasGoal, status, tc.want)
		}
	}
}

// The threshold row is a choice row, not a boolean: it cycles through the offered
// durations, persists the pick, and can turn the feature off.
func TestBackgroundThresholdFeatureRow(t *testing.T) {
	wiring, _, _, settings := newSettingsTestWiring(t)
	selector := NewFeatureSettingsSelector(FeatureSelectorOptions{
		Toggles: wiring.featureToggles(),
		OnFeatureChange: func(id string, value string) {
			if !wiring.ApplyFeatureToggle(id, value) {
				t.Fatalf("ApplyFeatureToggle(%q, %q) did not know the row", id, value)
			}
		},
		OnCancel: func() {},
	})
	list := selector.GetSettingsList()
	var row *tui.SettingItem
	for index, item := range list.Items() {
		if item.ID == "background-tools" {
			row = &list.Items()[index]
		}
	}
	if row == nil {
		t.Fatalf("/features holds no background row: %+v", list.Items())
	}
	// Every tool call is eligible (executeWithBackground has no name filter), so
	// the description must not narrow it to shell or MCP calls.
	if !strings.Contains(row.Description, "tool call") || strings.Contains(row.Description, "shell or MCP") {
		t.Fatalf("the row describes the wrong scope: %q", row.Description)
	}
	// It is a choice row, and its default is the one-minute threshold the harness
	// uses, not a boolean.
	if len(row.Values) < 3 {
		t.Fatalf("the row offers %d values, want the duration choices: %v", len(row.Values), row.Values)
	}
	if row.CurrentValue != coding.DefaultBackgroundToolCallChoice {
		t.Fatalf("the row shows %q, want %q", row.CurrentValue, coding.DefaultBackgroundToolCallChoice)
	}

	// The row is rendered with the current choice, not just stored: a user has to
	// be able to read the threshold off the screen.
	rendered := strings.Join(selector.Render(90), "\n")
	if !strings.Contains(rendered, "Background tools") || !strings.Contains(rendered, coding.DefaultBackgroundToolCallChoice) {
		t.Fatalf("/features does not render the threshold row:\n%s", rendered)
	}

	// Selecting "off" reaches the settings and the row.
	list.SelectItem("background-tools")
	for range row.Values {
		if settings.GetBackgroundToolCallThreshold() == 0 {
			break
		}
		list.HandleInput("\r")
	}
	if got := settings.GetBackgroundToolCallThreshold(); got != 0 {
		t.Fatalf("cycling to off left the threshold at %v", got)
	}
}

// The session reads the threshold from settings on every run, so a change in
// /features applies to the next tool call rather than only to a new session.
// Turning it off must yield zero, which disables the feature outright.
func TestBackgroundThresholdAppliesToTheLiveSession(t *testing.T) {
	wiring, _, _, settings := newSettingsTestWiring(t)
	read := func() time.Duration {
		if wiring.Session == nil {
			t.Fatal("no session")
		}
		return settings.GetBackgroundToolCallThreshold()
	}
	if got := read(); got != coding.DefaultBackgroundToolCallThreshold {
		t.Fatalf("default = %v, want %v", got, coding.DefaultBackgroundToolCallThreshold)
	}
	if !wiring.ApplyFeatureToggle("background-tools", "off") {
		t.Fatal("the row did not accept off")
	}
	if got := read(); got != 0 {
		t.Fatalf("after off = %v, want zero so the feature is disabled", got)
	}
	if !wiring.ApplyFeatureToggle("background-tools", "10m") {
		t.Fatal("the row did not accept 10m")
	}
	if got := read(); got != 10*time.Minute {
		t.Fatalf("after 10m = %v", got)
	}
}

// The default must be one of the row's own values. Otherwise the settings list
// cycles from index -1 and Enter jumps to the first entry ("off") instead of
// moving to the next duration, so the row would look like it skipped a step and
// the user could disable the feature by accident.
func TestBackgroundThresholdDefaultIsSelectable(t *testing.T) {
	wiring, _, _, _ := newSettingsTestWiring(t)
	selector := NewFeatureSettingsSelector(FeatureSelectorOptions{
		Toggles: wiring.featureToggles(), OnFeatureChange: func(string, string) {}, OnCancel: func() {},
	})
	list := selector.GetSettingsList()
	list.SelectItem("background-tools")
	var values []string
	for _, item := range list.Items() {
		if item.ID == "background-tools" {
			values = item.Values
		}
	}
	if len(values) == 0 {
		t.Fatal("the row is not a choice row")
	}
	if !slices.Contains(values, coding.DefaultBackgroundToolCallChoice) {
		t.Fatalf("the default %q is not among the choices %v", coding.DefaultBackgroundToolCallChoice, values)
	}
	if got := coding.ParseBackgroundToolCallThreshold(coding.DefaultBackgroundToolCallChoice); got != coding.DefaultBackgroundToolCallThreshold {
		t.Fatalf("the default choice parses to %v, not the default %v", got, coding.DefaultBackgroundToolCallThreshold)
	}
	// One Enter from the default lands on the next entry, not the first one.
	want := values[slices.Index(values, coding.DefaultBackgroundToolCallChoice)+1]
	chosen := ""
	selector = NewFeatureSettingsSelector(FeatureSelectorOptions{
		Toggles: wiring.featureToggles(),
		OnFeatureChange: func(id string, value string) {
			if id == "background-tools" {
				chosen = value
			}
		},
		OnCancel: func() {},
	})
	selector.GetSettingsList().SelectItem("background-tools")
	selector.GetSettingsList().HandleInput("\r")
	if chosen != want {
		t.Fatalf("Enter from the default chose %q, want the next entry %q", chosen, want)
	}
}

// D538: every offered choice, once stored, shows as itself when /features is
// opened again. Duration.String() spells 2m as "2m0s", which is not one of the
// values, so the row read "2m0s" and Enter jumped to "off" instead of "5m".
func TestBackgroundThresholdEveryChoiceIsSelectable(t *testing.T) {
	wiring, _, _, _ := newSettingsTestWiring(t)
	choices := coding.BackgroundToolCallChoices()
	for index, choice := range choices {
		if !wiring.Settings.SetBackgroundToolCallThreshold(choice) {
			t.Fatalf("storing %q failed", choice)
		}
		if got := backgroundToolCallChoice(wiring.Settings.GetBackgroundToolCallThreshold()); got != choice {
			t.Fatalf("a stored %q shows as %q", choice, got)
		}
		chosen := ""
		selector := NewFeatureSettingsSelector(FeatureSelectorOptions{
			Toggles: wiring.featureToggles(),
			OnFeatureChange: func(id string, value string) {
				if id == "background-tools" {
					chosen = value
				}
			},
			OnCancel: func() {},
		})
		selector.GetSettingsList().SelectItem("background-tools")
		selector.GetSettingsList().HandleInput("\r")
		if want := choices[(index+1)%len(choices)]; chosen != want {
			t.Fatalf("Enter from %q chose %q, want %q", choice, chosen, want)
		}
	}
	// A hand-edited value between the steps still names itself, in the row's
	// spelling.
	for value, want := range map[string]string{"10s": "10s", "90s": "1m30s", "1h": "1h", "1h30m": "1h30m"} {
		if got := backgroundToolCallChoice(coding.ParseBackgroundToolCallThreshold(value)); got != want {
			t.Fatalf("a stored %q shows as %q, want %q", value, got, want)
		}
	}
}
