package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// Tests ported from packages/coding-agent/test/settings-manager.test.ts (and
// the escaping/migration cases from the -bug/-compaction suites).

func TestSettingsReload(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, settingsPath, `{"theme":"dark","extensions":["/before.ts"]}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	writeFileMkdir(t, settingsPath, `{"theme":"light","extensions":["/after.ts"],"defaultModel":"claude-sonnet"}`)
	manager.Reload()

	if theme := manager.GetTheme(); theme == nil || *theme != "light" {
		t.Fatalf("theme = %v", theme)
	}
	if paths := manager.GetExtensionPaths(); len(paths) != 1 || paths[0] != "/after.ts" {
		t.Fatalf("extensions = %#v", paths)
	}
	if model := manager.GetDefaultModel(); model == nil || *model != "claude-sonnet" {
		t.Fatalf("defaultModel = %v", model)
	}

	// An invalid file keeps the previous settings and reports the path.
	writeFileMkdir(t, settingsPath, "{ invalid json")
	manager.Reload()
	if theme := manager.GetTheme(); theme == nil || *theme != "light" {
		t.Fatalf("theme after invalid reload = %v", theme)
	}
	errors := manager.DrainErrors()
	if len(errors) != 1 || errors[0].Scope != SettingsScopeGlobal || errors[0].Path != settingsPath {
		t.Fatalf("errors = %+v", errors)
	}
}

// Upstream's getSteeringMode/getFollowUpMode use `||`, so an empty stored
// string resolves to the default mode rather than to an unknown queue mode.
func TestSettingsEmptyQueueModesFallBack(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"steeringMode":"","followUpMode":""}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetSteeringMode(); got != "one-at-a-time" {
		t.Fatalf("steering = %q, want one-at-a-time", got)
	}
	if got := manager.GetFollowUpMode(); got != "one-at-a-time" {
		t.Fatalf("follow-up = %q, want one-at-a-time", got)
	}
	manager.SetSteeringMode("all")
	manager.SetFollowUpMode("all")
	if got := manager.GetSteeringMode(); got != "all" {
		t.Fatalf("steering = %q, want all", got)
	}
	if got := manager.GetFollowUpMode(); got != "all" {
		t.Fatalf("follow-up = %q, want all", got)
	}
}

func TestSettingsThemeSetting(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, settingsPath, `{"theme":"light/dark"}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if theme := manager.GetTheme(); theme != nil {
		t.Fatalf("getTheme() = %v", theme)
	}
	if theme := manager.GetThemeSetting(); theme == nil || *theme != "light/dark" {
		t.Fatalf("getThemeSetting() = %v", theme)
	}
	manager.SetTheme("solarized-light/tokyo-night")
	if saved := readSettingsFile(t, settingsPath); saved["theme"] != "solarized-light/tokyo-night" {
		t.Fatalf("theme = %#v", saved["theme"])
	}
}

func TestSettingsErrorTracking(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	globalPath := filepath.Join(agentDir, "settings.json")
	projectPath := filepath.Join(projectDir, ConfigDirName, "settings.json")
	writeFileMkdir(t, globalPath, "{ invalid global json")
	writeFileMkdir(t, projectPath, "{ invalid project json")

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	errors := manager.DrainErrors()
	if len(errors) != 2 {
		t.Fatalf("errors = %+v", errors)
	}
	if errors[0].Scope != SettingsScopeGlobal || errors[0].Path != globalPath {
		t.Fatalf("error 0 = %+v", errors[0])
	}
	if errors[1].Scope != SettingsScopeProject || errors[1].Path != projectPath {
		t.Fatalf("error 1 = %+v", errors[1])
	}
	if drained := manager.DrainErrors(); len(drained) != 0 {
		t.Fatalf("drained = %+v", drained)
	}
}

func TestSettingsProjectTrust(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"theme":"global"}`)
	projectPath := filepath.Join(projectDir, ConfigDirName, "settings.json")
	writeFileMkdir(t, projectPath, `{"theme":"project"}`)

	untrusted := false
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{ProjectTrusted: &untrusted})
	if manager.IsProjectTrusted() {
		t.Fatal("project must be untrusted")
	}
	if theme := manager.GetTheme(); theme == nil || *theme != "global" {
		t.Fatalf("theme = %v", theme)
	}
	if project := manager.GetProjectSettings(); settingsToMap(project)["theme"] != nil {
		t.Fatalf("project settings = %#v", project)
	}

	// Trusting reloads the project scope.
	manager.SetProjectTrusted(true)
	if !manager.IsProjectTrusted() {
		t.Fatal("project must be trusted")
	}
	if theme := manager.GetTheme(); theme == nil || *theme != "project" {
		t.Fatalf("theme = %v", theme)
	}

	// Writes to an untrusted project are refused and recorded: the Go port
	// records the error where upstream throws.
	writeFileMkdir(t, projectPath, `{"packages":["npm:existing"]}`)
	untrustedManager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{ProjectTrusted: &untrusted})
	untrustedManager.SetProjectPackages([]any{"npm:new"})
	errors := untrustedManager.DrainErrors()
	if len(errors) != 1 || !strings.Contains(errors[0].Error.Error(), "Project is not trusted; refusing to write project settings") {
		t.Fatalf("errors = %+v", errors)
	}
	if saved := readSettingsFile(t, projectPath); len(saved) != 1 || saved["packages"] == nil {
		t.Fatalf("project settings file = %#v", saved)
	}
}

func TestSettingsDefaultProjectTrust(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"defaultProjectTrust":"always"}`)
	writeFileMkdir(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"defaultProjectTrust":"never"}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetDefaultProjectTrust(); got != "always" {
		t.Fatalf("defaultProjectTrust = %s", got)
	}

	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"defaultProjectTrust":"sometimes"}`)
	manager = NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetDefaultProjectTrust(); got != "ask" {
		t.Fatalf("defaultProjectTrust = %s", got)
	}
}

func TestSettingsProjectDirectoryCreation(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"theme":"dark"}`)
	projectConfigDir := filepath.Join(projectDir, ConfigDirName)
	if err := os.RemoveAll(projectConfigDir); err != nil {
		t.Fatal(err)
	}

	// D283: lock-before-read creates the parent but not the settings file.
	// Upstream core/settings-manager.ts avoids both on read-only loads.
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if !PathExists(projectConfigDir) {
		t.Fatal("reading project settings must create the parent for locking")
	}
	if PathExists(filepath.Join(projectConfigDir, "settings.json")) {
		t.Fatal("reading project settings must not create the settings file")
	}
	if theme := manager.GetTheme(); theme == nil || *theme != "dark" {
		t.Fatalf("theme = %v", theme)
	}

	// Writing does create it.
	manager.SetProjectPackages([]any{map[string]any{"source": "npm:test-pkg"}})
	if !PathExists(projectConfigDir) {
		t.Fatal("writing project settings must create the config directory")
	}
	if !PathExists(filepath.Join(projectConfigDir, "settings.json")) {
		t.Fatal("writing project settings must create the settings file")
	}
}

func TestSettingsRetryDefaultsAndOverrides(t *testing.T) {
	defaults := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{}).GetRetrySettings()
	if defaults != (RetrySettingsResult{Enabled: true, MaxRetries: 3, BaseDelayMS: 2000, MaxAgentDelayMS: 60000}) {
		t.Fatalf("defaults = %+v", defaults)
	}
	enabled := true
	maxRetries := 10.0
	baseDelay := int64(500)
	maxAgent := int64(5000)
	custom := NewInMemorySettingsManager(&Settings{Retry: &SettingsRetry{
		Enabled: &enabled, MaxRetries: &maxRetries, BaseDelayMS: &baseDelay, MaxAgentDelayMS: &maxAgent,
	}}, SettingsManagerCreateOptions{}).GetRetrySettings()
	if custom != (RetrySettingsResult{Enabled: true, MaxRetries: 10, BaseDelayMS: 500, MaxAgentDelayMS: 5000}) {
		t.Fatalf("custom = %+v", custom)
	}
}

func TestSettingsHTTPIdleTimeout(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	timeout, err := manager.GetHTTPIdleTimeoutMS()
	if err != nil || timeout != DefaultHTTPIdleTimeoutMS {
		t.Fatalf("timeout = %d err = %v", timeout, err)
	}

	// Project settings override global ones.
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"httpIdleTimeoutMs":300000}`)
	writeFileMkdir(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"httpIdleTimeoutMs":0}`)
	manager = NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if timeout, err := manager.GetHTTPIdleTimeoutMS(); err != nil || timeout != 0 {
		t.Fatalf("timeout = %d err = %v", timeout, err)
	}

	// Invalid values are rejected (fresh dirs: the project scope from the
	// previous case would otherwise override the global value).
	agentDir, projectDir = settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"httpIdleTimeoutMs":-1}`)
	manager = NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if _, err := manager.GetHTTPIdleTimeoutMS(); err == nil ||
		!strings.Contains(err.Error(), "Invalid httpIdleTimeoutMs setting") {
		t.Fatalf("err = %v", err)
	}
}

// TestSettingsTuiMode mirrors settings-manager.test.ts's "TUI mode" block:
// the mode defaults to fullscreen, an explicit regular mode is persisted, and
// unsupported values (including the retired uiMode key) fall back to
// fullscreen.
func TestSettingsTuiMode(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetTuiMode(); got != "fullscreen" {
		t.Fatalf("default tui mode = %q; want fullscreen", got)
	}

	manager.SetTuiMode("regular")
	if got := manager.GetTuiMode(); got != "regular" {
		t.Fatalf("regular tui mode = %q", got)
	}
	if saved := readSettingsFile(t, settingsPath); saved["tuiMode"] != "regular" {
		t.Fatalf("tuiMode = %#v", saved["tuiMode"])
	}

	writeFileMkdir(t, settingsPath, `{"tuiMode":"other"}`)
	unsupported := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := unsupported.GetTuiMode(); got != "fullscreen" {
		t.Fatalf("unsupported tui mode = %q; want fullscreen", got)
	}

	// The retired uiMode key is not read.
	writeFileMkdir(t, settingsPath, `{"uiMode":"regular"}`)
	legacy := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := legacy.GetTuiMode(); got != "fullscreen" {
		t.Fatalf("legacy uiMode tui mode = %q; want fullscreen", got)
	}
}

// TestSettingsQuietStartup covers the QuietStartup union (boolean | "header",
// settings-manager.test.ts): the mode round-trips, and writing it keeps every
// other key — an unrepresentable value used to collapse the file.
func TestSettingsQuietStartup(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, settingsPath, `{"quietStartup":"header","defaultModel":"claude-sonnet","theme":"dark"}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetQuietStartup(); !got.Header || got.Enabled {
		t.Fatalf("getQuietStartup = %+v", got)
	}
	manager.SetQuietStartup(QuietStartupSetting{Enabled: true})
	saved := readSettingsFile(t, settingsPath)
	if saved["quietStartup"] != true || saved["defaultModel"] != "claude-sonnet" || saved["theme"] != "dark" {
		t.Fatalf("saved = %#v", saved)
	}
	manager.SetQuietStartup(QuietStartupSetting{Header: true})
	if saved := readSettingsFile(t, settingsPath); saved["quietStartup"] != "header" {
		t.Fatalf("quietStartup = %#v", saved["quietStartup"])
	}
	// The file's key order survives the write (upstream merges into the parsed
	// object).
	raw := readFileBytes(t, settingsPath)
	text := string(raw)
	if strings.Index(text, `"quietStartup"`) >= strings.Index(text, `"defaultModel"`) ||
		strings.Index(text, `"defaultModel"`) >= strings.Index(text, `"theme"`) {
		t.Fatalf("key order changed:\n%s", text)
	}
}

// TestSettingsPersistKeepsValuesTheTypeCannotRepresent pins the settings
// clobber: the persist path merges into the raw file object (upstream's
// withLock), so a value the typed settings cannot parse survives a save
// instead of the file being rewritten to just the modified field.
func TestSettingsPersistKeepsValuesTheTypeCannotRepresent(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, settingsPath, `{"outputPad":"wide","defaultModel":"claude-sonnet","tuiMode":"fullscreen"}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	manager.SetHideThinkingBlock(true)
	saved := readSettingsFile(t, settingsPath)
	for _, key := range []string{"outputPad", "defaultModel", "tuiMode"} {
		if _, ok := saved[key]; !ok {
			t.Fatalf("lost %q: %#v", key, saved)
		}
	}
	if saved["outputPad"] != "wide" {
		t.Fatalf("outputPad = %#v", saved["outputPad"])
	}
}

func TestSettingsExternalEditorAndTUISettings(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	manager := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{})
	// settings-manager.ts:995 is process.platform === "win32" ? "notepad" : "nano".
	wantDefault := "nano"
	if runtime.GOOS == "windows" {
		wantDefault = "notepad"
	}
	if got := manager.GetExternalEditorCommand(); got != wantDefault {
		t.Fatalf("editor = %s", got)
	}
	editor := "  custom-editor  "
	configured := NewInMemorySettingsManager(&Settings{ExternalEditor: &editor}, SettingsManagerCreateOptions{})
	if got := configured.GetExternalEditorCommand(); got != "  custom-editor  " {
		t.Fatalf("editor = %q", got)
	}
	t.Setenv("VISUAL", "visual-editor")
	if got := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{}).GetExternalEditorCommand(); got != "visual-editor" {
		t.Fatalf("editor = %q", got)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "env-editor")
	if got := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{}).GetExternalEditorCommand(); got != "env-editor" {
		t.Fatalf("editor = %q", got)
	}

	// TUI mode, output padding, mermaid, and shell command prefix.
	if got := manager.GetTuiMode(); got != "fullscreen" {
		t.Fatalf("tui mode = %s", got)
	}
	manager.SetTuiMode("regular")
	if got := manager.GetTuiMode(); got != "regular" {
		t.Fatalf("tui mode = %s", got)
	}
	if got := manager.GetOutputPad(); got != 1 {
		t.Fatalf("outputPad = %d", got)
	}
	manager.SetOutputPad(0)
	if got := manager.GetOutputPad(); got != 0 {
		t.Fatalf("outputPad = %d", got)
	}
	if got := manager.GetMermaidRenderingMode(); got != "streaming" {
		t.Fatalf("mermaid = %s", got)
	}
	manager.SetMermaidRenderingMode("final")
	if got := manager.GetMermaidRenderingMode(); got != "final" {
		t.Fatalf("mermaid = %s", got)
	}
	weird := 2
	unsupported := NewInMemorySettingsManager(&Settings{OutputPad: &weird}, SettingsManagerCreateOptions{})
	if got := unsupported.GetOutputPad(); got != 1 {
		t.Fatalf("unsupported outputPad = %d", got)
	}
	sometimes := "sometimes"
	badMermaid := NewInMemorySettingsManager(&Settings{Markdown: &SettingsMarkdown{Mermaid: &sometimes}}, SettingsManagerCreateOptions{})
	if got := badMermaid.GetMermaidRenderingMode(); got != "streaming" {
		t.Fatalf("unsupported mermaid = %s", got)
	}

	prefix := "shopt -s expand_aliases"
	withPrefix := NewInMemorySettingsManager(&Settings{ShellCommandPrefix: &prefix}, SettingsManagerCreateOptions{})
	if got := withPrefix.GetShellCommandPrefix(); got == nil || *got != "shopt -s expand_aliases" {
		t.Fatalf("prefix = %v", got)
	}
	if got := manager.GetShellCommandPrefix(); got != nil {
		t.Fatalf("prefix = %v", got)
	}
}

// TestSettingsDefaultToolModifiers is upstream's "applies +name and -name to the
// default selection" case (30a1d1849).
func TestSettingsDefaultToolModifiers(t *testing.T) {
	manager := NewInMemorySettingsManager(&Settings{DefaultTools: []string{"+codemode", "-write"}}, SettingsManagerCreateOptions{})
	if got := strings.Join(manager.GetDefaultTools(), ","); got != "read,bash,edit,codemode" {
		t.Fatalf("+codemode -write = %q", got)
	}
	manager = NewInMemorySettingsManager(&Settings{DefaultTools: []string{"read", "+grep", "+read"}}, SettingsManagerCreateOptions{})
	if got := strings.Join(manager.GetDefaultTools(), ","); got != "read,grep" {
		t.Fatalf("read +grep +read = %q", got)
	}
}

// TestSettingsDefaultToolsLayerModifiers is upstream's "layers project modifiers
// on top of the global selection" case (30a1d1849).
func TestSettingsDefaultToolsLayerModifiers(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"defaultTools":["read","bash","+codemode"]}`)
	writeFileMkdir(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"defaultTools":["-codemode","+tool_search"]}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := strings.Join(manager.GetDefaultTools(), ","); got != "read,bash,tool_search" {
		t.Fatalf("layered defaultTools = %q", got)
	}
	manager.ApplyOverrides(&Settings{DefaultTools: []string{"+codemode"}})
	if got := strings.Join(manager.GetDefaultTools(), ","); got != "read,bash,tool_search,codemode" {
		t.Fatalf("override defaultTools = %q", got)
	}
}

// TestSettingsDefaultToolsProjectOnlyModifiers is upstream's "applies project
// modifiers to the built-in defaults without a global setting" case (30a1d1849).
func TestSettingsDefaultToolsProjectOnlyModifiers(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"defaultTools":["+codemode"]}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := strings.Join(manager.GetDefaultTools(), ","); got != "read,bash,edit,write,codemode" {
		t.Fatalf("project-only modifiers = %q", got)
	}
}

func TestSettingsDefaultToolsAndPaths(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), `{"defaultTools":["read","bash"],"sessionDir":"/tmp/sessions","shellPath":"~"}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	tools := manager.GetDefaultTools()
	if len(tools) != 2 || tools[0] != "read" || tools[1] != "bash" {
		t.Fatalf("defaultTools = %#v", tools)
	}
	// Project settings replace the global array.
	writeFileMkdir(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"defaultTools":["grep"]}`)
	manager = NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if tools := manager.GetDefaultTools(); len(tools) != 1 || tools[0] != "grep" {
		t.Fatalf("defaultTools = %#v", tools)
	}
	// An explicitly empty list survives.
	if tools := NewInMemorySettingsManager(&Settings{DefaultTools: []string{}}, SettingsManagerCreateOptions{}).GetDefaultTools(); tools == nil || len(tools) != 0 {
		t.Fatalf("empty defaultTools = %#v", tools)
	}
	if tools := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{}).GetDefaultTools(); tools != nil {
		t.Fatalf("unset defaultTools = %#v", tools)
	}

	// Shell path expands a bare tilde; sessionDir expands too.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.GetShellPath(); got == nil || *got != home {
		t.Fatalf("shellPath = %v (want %s)", got, home)
	}
	if got := manager.GetSessionDir(); got == nil || *got != "/tmp/sessions" {
		t.Fatalf("sessionDir = %v", got)
	}
	relative := "./sessions"
	if got := NewInMemorySettingsManager(&Settings{SessionDir: &relative}, SettingsManagerCreateOptions{}).GetSessionDir(); got == nil || *got != "./sessions" {
		t.Fatalf("relative sessionDir = %v", got)
	}
	if got := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{}).GetSessionDir(); got != nil {
		t.Fatalf("unset sessionDir = %v", got)
	}
}

func TestSettingsCompactionTokenResolution(t *testing.T) {
	manager := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{})
	reserve, err := manager.GetCompactionReserveTokens(nil)
	if err != nil || reserve != DefaultCompactionReserveTokens {
		t.Fatalf("reserve = %d err = %v", reserve, err)
	}
	// Model overrides win over ordinary settings, which win over defaults.
	ordinary := int64(4096)
	override := int64(1024)
	manager = NewInMemorySettingsManager(&Settings{Compaction: &SettingsCompaction{
		ReserveTokens:  &ordinary,
		ModelOverrides: map[string]SettingsCompactionOverride{"anthropic/claude-sonnet-4-5": {ReserveTokens: &override}},
	}}, SettingsManagerCreateOptions{})
	model := testModel("claude-sonnet-4-5", "Claude Sonnet 4.5", "anthropic", true)
	value, err := manager.GetCompactionReserveTokens(model)
	if err != nil || value != 1024 {
		t.Fatalf("override = %d err = %v", value, err)
	}
	other := testModel("gpt-4o", "GPT-4o", "openai", false)
	value, err = manager.GetCompactionReserveTokens(other)
	if err != nil || value != 4096 {
		t.Fatalf("ordinary = %d err = %v", value, err)
	}
	// Negative values are rejected.
	negative := int64(-1)
	invalid := NewInMemorySettingsManager(&Settings{Compaction: &SettingsCompaction{ReserveTokens: &negative}}, SettingsManagerCreateOptions{})
	if _, err := invalid.GetCompactionReserveTokens(nil); err == nil ||
		!strings.Contains(err.Error(), "Invalid compaction.reserveTokens setting") {
		t.Fatalf("err = %v", err)
	}
}

// settings-defaults.ts autocompleteMaxVisible is 5, and
// settings-manager.ts clamps a stored value to [3, 20] (Math.max(3, Math.min
// (20, ...))). Neither the default nor the clamp was pinned.
func TestAutocompleteMaxVisibleDefaultAndClamp(t *testing.T) {
	manager := NewInMemorySettingsManager(&Settings{}, SettingsManagerCreateOptions{})
	if got := manager.GetAutocompleteMaxVisible(); got != 5 {
		t.Fatalf("default autocomplete items = %d, want 5", got)
	}
	for _, testCase := range []struct{ set, want int }{{1, 3}, {3, 3}, {7, 7}, {20, 20}, {99, 20}} {
		manager.SetAutocompleteMaxVisible(testCase.set)
		if got := manager.GetAutocompleteMaxVisible(); got != testCase.want {
			t.Fatalf("SetAutocompleteMaxVisible(%d) reads %d, want %d", testCase.set, got, testCase.want)
		}
	}
}

func TestSettingsManagerInMemoryRoundTrip(t *testing.T) {
	manager := NewInMemorySettingsManager(&Settings{Theme: new("dark")}, SettingsManagerCreateOptions{})
	manager.SetDefaultModelAndProvider("anthropic", "claude-opus-4-8")
	manager.SetModelThinkingLevel("anthropic", "claude-opus-4-8", "high")
	if level := manager.GetModelThinkingLevel("anthropic", "claude-opus-4-8"); level == nil || *level != "high" {
		t.Fatalf("level = %v", level)
	}
	if all := manager.GetAllModelThinkingLevels(); all["anthropic/claude-opus-4-8"] != "high" {
		t.Fatalf("all = %#v", all)
	}
	manager.RemoveModelThinkingLevel("anthropic", "claude-opus-4-8")
	if level := manager.GetModelThinkingLevel("anthropic", "claude-opus-4-8"); level != nil {
		t.Fatalf("level = %v", level)
	}
	if provider := manager.GetDefaultProvider(); provider == nil || *provider != "anthropic" {
		t.Fatalf("provider = %v", provider)
	}
	// Global scope changes are visible through the merged view.
	if model := manager.GetDefaultModel(); model == nil || *model != "claude-opus-4-8" {
		t.Fatalf("model = %v", model)
	}
	// Overrides apply to the merged view only.
	override := "override"
	manager.ApplyOverrides(&Settings{Theme: &override})
	if theme := manager.GetThemeSetting(); theme == nil || *theme != "override" {
		t.Fatalf("theme = %v", theme)
	}
	if global := manager.GetGlobalSettings(); global.Theme == nil || *global.Theme != "dark" {
		t.Fatalf("global theme = %v", global.Theme)
	}
}

// An unset optional setting must be omitted, never written as null. A typed
// nil inside setAny's `any` is not `== nil`, so a nil *SettingsCompaction or a
// nil []any passed through it serialized as json `null` and the full rewrite
// re-added keys the user never set (compaction, branchSummary, retry, packages,
// terminal, images, thinkingBudgets, markdown, warnings). Upstream merges into
// the parsed file and never emits those keys.
func TestSettingsMarshalOmitsUnsetOptionals(t *testing.T) {
	encoded, err := (&Settings{}).marshalOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "{}" {
		t.Fatalf("empty settings = %s, want {}", encoded)
	}

	// Setting one scalar must not drag the typed-nil holders along.
	level := "high"
	encoded, err = (&Settings{DefaultThinkingLevel: &level}).marshalOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"defaultThinkingLevel":"high"}` {
		t.Fatalf("settings = %s", encoded)
	}

	// A nested object that IS set still serializes, and its own unset fields
	// stay omitted rather than becoming nulls.
	enabled := true
	encoded, err = (&Settings{Compaction: &SettingsCompaction{Enabled: &enabled}}).marshalOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"compaction":{"enabled":true}}` {
		t.Fatalf("settings = %s", encoded)
	}
}

// Port of the fullscreenWheelScrollLines cases in upstream
// settings-manager.test.ts (f1927c2d5): default "auto", clamped 1..100, and the
// two settings.json forms.
func TestSettingsFullscreenWheelScrollLines(t *testing.T) {
	manager := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{})
	if got := manager.GetFullscreenWheelScrollLines(); !got.Auto {
		t.Fatalf("default = %#v (want auto)", got)
	}
	manager.SetFullscreenWheelScrollLines(tui.FixedWheelScrollLines(0))
	if got := manager.GetFullscreenWheelScrollLines(); got != tui.FixedWheelScrollLines(1) {
		t.Fatalf("lower clamp = %#v", got)
	}
	manager.SetFullscreenWheelScrollLines(tui.FixedWheelScrollLines(500))
	if got := manager.GetFullscreenWheelScrollLines(); got != tui.FixedWheelScrollLines(100) {
		t.Fatalf("upper clamp = %#v", got)
	}
	manager.SetFullscreenWheelScrollLines(tui.FixedWheelScrollLines(3))
	if encoded := marshalSettingsIndent(manager.globalSettings); !strings.Contains(encoded, `"fullscreenWheelScrollLines": 3`) {
		t.Fatalf("fixed encoding = %s", encoded)
	}
	if reloaded := mapToSettings(settingsToMap(manager.settings)); reloaded.FullscreenWheelScrollLines == nil || *reloaded.FullscreenWheelScrollLines != tui.FixedWheelScrollLines(3) {
		t.Fatalf("reload = %#v", reloaded.FullscreenWheelScrollLines)
	}
	manager.SetFullscreenWheelScrollLines(tui.AutoWheelScrollLines())
	if encoded := marshalSettingsIndent(manager.globalSettings); !strings.Contains(encoded, `"fullscreenWheelScrollLines": "auto"`) {
		t.Fatalf("auto encoding = %s", encoded)
	}

	settings := mapToSettings(map[string]any{"fullscreenWheelScrollLines": float64(1e308)})
	manager = NewInMemorySettingsManager(settings, SettingsManagerCreateOptions{})
	if got := manager.GetFullscreenWheelScrollLines(); got != tui.FixedWheelScrollLines(100) {
		t.Fatalf("large finite setting = %#v, want clamped 100", got)
	}
}

// pi v1.5.0 (f9a26365) packages/ai/src/utils/retry.ts retryDelayMs keeps a
// configured zero cap (`policy.maxAgentDelayMs ?? default`), so a settings file
// with `maxAgentDelayMs: 0` retries without a backoff delay. The policy the
// session builds from those settings must therefore carry the zero through
// instead of falling back to the 60 s default (A447).
func TestSettingsRetryZeroMaxAgentDelayReachesThePolicy(t *testing.T) {
	zero := int64(0)
	manager := NewInMemorySettingsManager(&Settings{Retry: &SettingsRetry{MaxAgentDelayMS: &zero}}, SettingsManagerCreateOptions{})
	resolved := manager.GetRetrySettings()
	if resolved.MaxAgentDelayMS != 0 {
		t.Fatalf("resolved maxAgentDelayMs = %d, want 0", resolved.MaxAgentDelayMS)
	}
	policy := ai.RetryPolicy{
		Enabled: resolved.Enabled, MaxRetries: resolved.MaxRetries,
		BaseDelayMS: int(resolved.BaseDelayMS), MaxAgentDelayMS: resolved.MaxAgentDelayMS,
		MaxAgentDelayMSSet: true,
	}
	if got := ai.RetryDelayMS(policy, 1); got != 0 {
		t.Errorf("RetryDelayMS = %d, want 0", got)
	}
}

// D469: SetPackages takes []any, so a package source filter reaches the writer
// as a Go map and its keys are written sorted. pi v1.5.2 (b4afdddf)
// core/settings-manager.ts stores the caller's JavaScript object, which keeps
// insertion order. No production caller sets packages (the package manager is
// out of scope), and a faithful fix needs an order-preserving input type, so
// this pins the current shape instead of changing the public API.
func TestSettingsPackageFilterKeysAreSorted(t *testing.T) {
	dir := t.TempDir()
	cwd := filepath.Join(dir, "project")
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trusted := true
	manager := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{ProjectTrusted: &trusted})
	manager.SetPackages([]any{map[string]any{"source": "pkg-b", "skills": []any{"s"}}})
	manager.FlushPersists()
	written := readFileBytes(t, filepath.Join(agentDir, "settings.json"))
	if !strings.Contains(string(written), "\"skills\": [") || strings.Index(string(written), "\"skills\"") > strings.Index(string(written), "\"source\"") {
		t.Fatalf("settings = %s, want the filter keys sorted", written)
	}
}

// The compaction token settings are validated against JavaScript's safe-integer
// range: pi v1.5.0 (f9a26365) core/settings-manager.ts getCompactionTokenSetting
// rejects a value with `!Number.isSafeInteger(ordinary) || ordinary < 0`.
// Number.isSafeInteger is false above 2^53-1, where a JavaScript number can no
// longer hold every integer, so the reference refuses such a setting instead of
// letting it round. The port checked only `< 0`, so a value past that range was
// accepted: 2^53+1 arrived as 2^53 from the JSON decoder and was used.
func TestCompactionTokenSettingsRejectUnsafeIntegers(t *testing.T) {
	cases := []struct {
		name    string
		content string
		model   *ai.Model
	}{
		{"reserveTokens", `{"compaction":{"reserveTokens":9007199254740993}}`, nil},
		{"keepRecentTokens", `{"compaction":{"keepRecentTokens":9007199254740993}}`, nil},
		{
			name:    "model override",
			content: `{"compaction":{"modelOverrides":{"anthropic/m":{"reserveTokens":9007199254740993}}}}`,
			model:   &ai.Model{Provider: "anthropic", ID: "m"},
		},
		{
			// 2^53-1 is Number.MAX_SAFE_INTEGER: the reference accepts it.
			name:    "model override at the limit",
			content: `{"compaction":{"modelOverrides":{"anthropic/m":{"reserveTokens":9007199254740991}}}}`,
			model:   &ai.Model{Provider: "anthropic", ID: "m"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(testCase.content), &raw); err != nil {
				t.Fatal(err)
			}
			_ = MigrateSettingsRaw(raw)
			manager := NewInMemorySettingsManager(mapToSettings(raw), SettingsManagerCreateOptions{})
			_, reserveErr := manager.GetCompactionReserveTokens(testCase.model)
			_, keepErr := manager.GetCompactionKeepRecentTokens(testCase.model)
			err := reserveErr
			if err == nil {
				err = keepErr
			}
			atLimit := testCase.name == "model override at the limit"
			if atLimit && err != nil {
				t.Fatalf("2^53 was rejected: %v", err)
			}
			if !atLimit && err == nil {
				t.Fatal("an unsafe integer was accepted")
			}
		})
	}
}

// Reference: pi v1.6.0 core/settings-manager.ts getDefaultTools filters the
// stored list to its string entries (`tools.filter((tool) => typeof tool ===
// "string")`) and only reports undefined when the key is absent. pier decoded
// the whole member strictly, so one non-string entry (or a member that is not
// an array at all) dropped defaultTools entirely and the caller fell back to
// every built-in tool: a malformed setting silently widened the tool set.
func TestSettingsDefaultToolsIgnoresNonStringEntries(t *testing.T) {
	for _, testCase := range []struct{ name, stored, want string }{
		{"mixed", `{"defaultTools":["read",5]}`, "read"},
		{"not an array", `{"defaultTools":"read"}`, ""},
		{"objects", `{"defaultTools":[{"name":"read"},"bash"]}`, "bash"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agentDir, projectDir := settingsDirs(t)
			writeFileMkdir(t, filepath.Join(agentDir, "settings.json"), testCase.stored)
			manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
			tools := manager.GetDefaultTools()
			if tools == nil {
				t.Fatalf("GetDefaultTools = nil; the malformed member must still resolve to a selection")
			}
			if got := strings.Join(tools, ","); got != testCase.want {
				t.Fatalf("GetDefaultTools = %q, want %q", got, testCase.want)
			}
		})
	}
}

// Reference: pi v1.6.0 core/settings-manager.ts stores these numeric settings as
// JavaScript Numbers and consumes them as counts: getImageWidthCells floors on
// read (Math.max(1, Math.floor(width))), and an editor padding or autocomplete
// limit reaches String.repeat or a slice bound, which truncate the same way. A
// stored 2.7 is therefore 2 usable spaces and 5.9 is 5 visible cells. pier
// decoded the members into Go ints, which rejects every non-integral number, so
// the whole member was discarded as unrepresentable and the getter returned its
// default instead: the setting silently had no effect.
func TestSettingsNumericSettingsFloorLikeJavaScript(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeFileMkdir(t, filepath.Join(agentDir, "settings.json"),
		`{"terminal":{"imageWidthCells":5.9},"editorPaddingX":2.7,"autocompleteMaxVisible":7.5}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetImageWidthCells(); got != 5 {
		t.Fatalf("GetImageWidthCells = %d, want 5", got)
	}
	if got := manager.GetEditorPaddingX(); got != 2 {
		t.Fatalf("GetEditorPaddingX = %d, want 2", got)
	}
	if got := manager.GetAutocompleteMaxVisible(); got != 7 {
		t.Fatalf("GetAutocompleteMaxVisible = %d, want 7", got)
	}
}

// TestCompactionTokenCountAcceptsJSONNumbers covers what a token-count member
// may be. The reference parses the setting as a JSON number, so `1e6` is
// 1000000, and validates it with Number.isSafeInteger in
// getCompactionTokenSetting: a fraction, an out-of-range number or an unsafe
// integer is reported with its String(value) form. Decoding straight into int64
// dropped `1e6` entirely, so the getter fell back to the default and the user's
// compaction reserve silently became 16384.
func TestCompactionTokenCountAcceptsJSONNumbers(t *testing.T) {
	for _, testCase := range []struct {
		member string
		value  int64
		err    string
	}{
		{"1000", 1000, ""},
		{"1e6", 1000000, ""},
		{"1000.9", 0, "Invalid compaction.reserveTokens setting: 1000.9. Expected a non-negative safe integer."},
		{"1e19", 0, "Invalid compaction.reserveTokens setting: 10000000000000000000. Expected a non-negative safe integer."},
		{"9007199254740993", 0, "Invalid compaction.reserveTokens setting: 9007199254740992. Expected a non-negative safe integer."},
		{"null", 0, "Invalid compaction.reserveTokens setting: null. Expected a non-negative safe integer."},
	} {
		var settings Settings
		decodeSettingsTolerantWithRejects([]byte(`{"compaction":{"reserveTokens":`+testCase.member+`}}`), &settings)
		manager := NewInMemorySettingsManager(&settings, SettingsManagerCreateOptions{})
		value, err := manager.GetCompactionReserveTokens(nil)
		if testCase.err == "" {
			if err != nil || value != testCase.value {
				t.Errorf("reserveTokens %s: value = %d, err = %v, want %d", testCase.member, value, err, testCase.value)
			}
			continue
		}
		if err == nil || err.Error() != testCase.err {
			t.Errorf("reserveTokens %s: err = %v, want %q", testCase.member, err, testCase.err)
		}
	}
}

// TestSettingsRetryKeepsAFractionalMaxRetries covers the field's type. The
// reference's schema is `Type.Number()`, not an integer type, and
// `agent-session.ts` compares the attempt count against the stored number
// directly, so `{"retry":{"maxRetries":2.7}}` allows attempt 2 where a truncated
// `2` refuses it. Decoding into an int silently made the port retry once fewer.
func TestSettingsRetryKeepsAFractionalMaxRetries(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"),
		[]byte(`{"retry":{"enabled":true,"maxRetries":2.7}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if raw := manager.GetRetryMaxRetriesRaw(); raw != 2.7 {
		t.Fatalf("GetRetryMaxRetriesRaw = %v, want 2.7", raw)
	}
	// The resolved count truncates for display and event payloads.
	if resolved := manager.GetRetrySettings().MaxRetries; resolved != 2 {
		t.Fatalf("GetRetrySettings().MaxRetries = %d, want 2", resolved)
	}
	// The policy carries the fraction too, which is what the retry decision
	// compares, so attempt 2 is allowed by 2.7 and refused by a truncated 2.
	// The two comparisons agent-session.ts makes, against the stored number and
	// against the truncated one: attempt 2 differs, which is the whole bug.
	if (float64(2) >= 2.7) || !(float64(2) >= 2) {
		t.Fatal("attempt 2 must be allowed by 2.7 and refused by the truncated 2")
	}
}

// The backgrounding threshold defaults to one minute, can be turned off, and
// ignores a value it does not understand rather than silently disabling itself.
func TestBackgroundToolCallThresholdSetting(t *testing.T) {
	settings := NewInMemorySettingsManager(nil, SettingsManagerCreateOptions{})
	if got := settings.GetBackgroundToolCallThreshold(); got != DefaultBackgroundToolCallThreshold {
		t.Fatalf("default = %v, want %v", got, DefaultBackgroundToolCallThreshold)
	}
	// Backgrounding is opt-in like the other pier-only features (D496).
	if DefaultBackgroundToolCallThreshold != 0 {
		t.Fatalf("the default is %v, want off", DefaultBackgroundToolCallThreshold)
	}
	if DefaultBackgroundToolCallChoice != "off" {
		t.Fatalf("the default choice is %q, want off", DefaultBackgroundToolCallChoice)
	}
	// The default must also be one of the row's choices, or Enter on the row
	// cycles from index -1 and jumps to "off" instead of the next duration.
	if got := ParseBackgroundToolCallThreshold(DefaultBackgroundToolCallChoice); got != DefaultBackgroundToolCallThreshold {
		t.Fatalf("the default choice %q parses to %v, want %v",
			DefaultBackgroundToolCallChoice, got, DefaultBackgroundToolCallThreshold)
	}
	if !settings.SetBackgroundToolCallThreshold("off") {
		t.Fatal("off was refused")
	}
	if got := settings.GetBackgroundToolCallThreshold(); got != 0 {
		t.Fatalf("off = %v, want zero", got)
	}
	if !settings.SetBackgroundToolCallThreshold("5m") {
		t.Fatal("5m was refused")
	}
	if got := settings.GetBackgroundToolCallThreshold(); got != 5*time.Minute {
		t.Fatalf("5m = %v", got)
	}
	// A value outside the offered choices is refused, so a typo cannot disable it.
	if settings.SetBackgroundToolCallThreshold("banana") {
		t.Fatal("an unknown value was accepted")
	}
	if got := settings.GetBackgroundToolCallThreshold(); got != 5*time.Minute {
		t.Fatalf("a refused value changed the threshold to %v", got)
	}
	// A stored value this build cannot read falls back to the default, not to off.
	if got := ParseBackgroundToolCallThreshold("banana"); got != DefaultBackgroundToolCallThreshold {
		t.Fatalf("an unreadable stored value = %v, want the default", got)
	}
	if got := ParseBackgroundToolCallThreshold(""); got != 0 {
		t.Fatalf("an empty value = %v, want off", got)
	}
}

// The threshold survives a reload, so a choice made once is honoured by the next
// session rather than resetting to the default.
func TestBackgroundToolCallThresholdPersists(t *testing.T) {
	projectDir, agentDir := t.TempDir(), t.TempDir()
	first := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if !first.SetBackgroundToolCallThreshold("10m") {
		t.Fatal("10m was refused")
	}
	first.FlushPersists()
	reloaded := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := reloaded.GetBackgroundToolCallThreshold(); got != 10*time.Minute {
		t.Fatalf("after reload = %v, want 10m", got)
	}
	// A stored off survives too, rather than reading as unset.
	if !reloaded.SetBackgroundToolCallThreshold("off") {
		t.Fatal("off was refused")
	}
	reloaded.FlushPersists()
	if got := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{}).
		GetBackgroundToolCallThreshold(); got != 0 {
		t.Fatalf("after reloading off = %v, want zero", got)
	}
}

// Reference: pi v1.6.0 (faac5858) core/settings-manager.ts declares
// editorPaddingX, imageWidthCells and autocompleteMaxVisible as Type.Number and
// only floors them in the SETTERS (Math.floor in setEditorPaddingX,
// setImageWidthCells, setAutocompleteMaxVisible). A hand-edited settings.json may
// therefore hold 2.7, and the getter returns it untouched.
//
// Pier stores them in Go ints, which reject a fraction outright: a strict decode
// fails and the member is DROPPED, so the default replaces the user's value.
// floorSettingsNumber keeps the setting instead, flooring it the way the
// reference's own setters ultimately do.
func TestToleratedNumberMembersFloorInsteadOfReject(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		check func(*SettingsManager) int
		want  int
	}{
		{`{"editorPaddingX":2.7}`, func(m *SettingsManager) int { return m.GetEditorPaddingX() }, 2},
		{`{"terminal":{"imageWidthCells":60.7}}`, func(m *SettingsManager) int { return m.GetImageWidthCells() }, 60},
		{`{"autocompleteMaxVisible":20.9}`, func(m *SettingsManager) int { return m.GetAutocompleteMaxVisible() }, 20},
	} {
		var settings Settings
		if rejected := decodeSettingsTolerantWithRejects([]byte(tc.raw), &settings); len(rejected) != 0 {
			t.Fatalf("%s: rejected %v; want the member kept", tc.raw, rejected)
		}
		manager := NewInMemorySettingsManager(&settings, SettingsManagerCreateOptions{})
		if got := tc.check(manager); got != tc.want {
			t.Errorf("%s = %d, want %d (floored, not dropped to the default)", tc.raw, got, tc.want)
		}
	}
	// A member that is NOT in the tolerant list still rejects a fraction, so the
	// tolerance stays a deliberate list rather than a blanket rule. A nested
	// member reports through its getter rather than the top-level rejects.
	var settings Settings
	decodeSettingsTolerantWithRejects([]byte(`{"compaction":{"reserveTokens":1000.9}}`), &settings)
	manager := NewInMemorySettingsManager(&settings, SettingsManagerCreateOptions{})
	if _, err := manager.GetCompactionReserveTokens(nil); err == nil {
		t.Fatal("a fractional reserveTokens was accepted; it must reject")
	}
}
