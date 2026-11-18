package coding

import (
	"encoding/json"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests ported from packages/coding-agent/test/settings-manager.test.ts (and
// the escaping/migration cases from the -bug/-compaction suites).

func settingsDirs(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent")
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	return agentDir, projectDir
}

func writeSettingsFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSettingsFile(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSettingsPreservesExternallyAddedSettings(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeSettingsFile(t, settingsPath, `{"theme":"dark","defaultModel":"claude-sonnet"}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})

	// The user edits settings.json externally to add enabledModels.
	raw := readSettingsFile(t, settingsPath)
	raw["enabledModels"] = []any{"claude-opus-4-5", "gpt-5.2-codex"}
	encoded, _ := json.Marshal(raw)
	writeSettingsFile(t, settingsPath, string(encoded))

	manager.SetDefaultThinkingLevel("high")
	saved := readSettingsFile(t, settingsPath)
	if saved["defaultThinkingLevel"] != "high" || saved["theme"] != "dark" || saved["defaultModel"] != "claude-sonnet" {
		t.Fatalf("saved = %#v", saved)
	}
	models, _ := saved["enabledModels"].([]any)
	if len(models) != 2 || models[0] != "claude-opus-4-5" || models[1] != "gpt-5.2-codex" {
		t.Fatalf("enabledModels = %#v", saved["enabledModels"])
	}

	// Unrelated custom settings survive a theme write too, including the
	// v1.2.0 `$schema` reference for editor validation.
	raw = readSettingsFile(t, settingsPath)
	raw["shellPath"] = "/bin/zsh"
	raw["extensions"] = []any{"/path/to/extension.ts"}
	raw["$schema"] = "https://pi.dev/schemas/settings.schema.json"
	encoded, _ = json.Marshal(raw)
	writeSettingsFile(t, settingsPath, string(encoded))
	manager.SetTheme("light")
	saved = readSettingsFile(t, settingsPath)
	if saved["theme"] != "light" || saved["shellPath"] != "/bin/zsh" ||
		saved["$schema"] != "https://pi.dev/schemas/settings.schema.json" {
		t.Fatalf("saved = %#v", saved)
	}
	extensions, _ := saved["extensions"].([]any)
	if len(extensions) != 1 || extensions[0] != "/path/to/extension.ts" {
		t.Fatalf("extensions = %#v", saved["extensions"])
	}
}

func TestSettingsPackagesMigration(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")

	// Local-only extensions stay in the extensions array.
	writeSettingsFile(t, settingsPath, `{"extensions":["/local/ext.ts","./relative/ext.ts"]}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if packages := manager.GetPackages(); len(packages) != 0 {
		t.Fatalf("packages = %#v", packages)
	}
	if paths := manager.GetExtensionPaths(); len(paths) != 2 || paths[0] != "/local/ext.ts" || paths[1] != "./relative/ext.ts" {
		t.Fatalf("extensions = %#v", paths)
	}

	// Package entries keep their filtering objects.
	writeSettingsFile(t, settingsPath, `{"packages":["npm:simple-pkg",{"source":"npm:shitty-extensions","extensions":["extensions/oracle.ts"],"skills":[]}]}`)
	manager = NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	packages := manager.GetPackages()
	if len(packages) != 2 || packages[0] != "npm:simple-pkg" {
		t.Fatalf("packages = %#v", packages)
	}
	filter, ok := packages[1].(map[string]any)
	if !ok || filter["source"] != "npm:shitty-extensions" {
		t.Fatalf("filter = %#v", packages[1])
	}
	extensions, _ := filter["extensions"].([]any)
	if len(extensions) != 1 || extensions[0] != "extensions/oracle.ts" {
		t.Fatalf("filter extensions = %#v", filter["extensions"])
	}
	if skills, ok := filter["skills"].([]any); !ok || len(skills) != 0 {
		t.Fatalf("filter skills = %#v", filter["skills"])
	}
}

// Reference: pi v1.4.0 (4a080252) core/settings-manager.ts deepMergeObjects
// skips undefined overrides but lets an explicit null project value replace
// the global setting. A null is therefore a project-level reset.
func TestSettingsProjectNullMasksGlobalDefaultProvider(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"), `{"defaultProvider":"openai"}`)
	writeSettingsFile(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"defaultProvider":null}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetDefaultProvider(); got != nil {
		t.Fatalf("project null left inherited default provider %q; want unset", *got)
	}
}

// Nulls inside mergeable settings objects also replace inherited values in
// pi v1.4.0 (4a080252) core/settings-manager.ts deepMergeObjects.
func TestSettingsProjectNestedNullMasksGlobalCompactionEnabled(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"), `{"compaction":{"enabled":false}}`)
	writeSettingsFile(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"compaction":{"enabled":null}}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if got := manager.GetCompactionEnabled(); !got {
		t.Fatal("project nested null retained the global false compaction setting; want upstream default true")
	}
}

// pi v1.4.0 (4a080252) core/settings-manager.ts rejects null token settings
// instead of treating them as an absent override.
// Reference: pi v1.4.0 (4a080252) core/settings-manager.ts setCompactionEnabled.
// A later setter replaces an explicit null with the new boolean.
func TestSettingsCompactionSetterReplacesNull(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeSettingsFile(t, settingsPath, `{"compaction":{"enabled":null}}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	manager.SetCompactionEnabled(false)
	if manager.GetCompactionEnabled() {
		t.Fatal("setting compaction false after null left it enabled")
	}
	if saved := readSettingsFile(t, settingsPath); saved["compaction"].(map[string]any)["enabled"] != false {
		t.Fatalf("saved compaction.enabled = %#v; want false", saved["compaction"].(map[string]any)["enabled"])
	}
}

// Nested struct nulls replace inherited values while sibling project values
// remain intact (pi v1.4.0 (4a080252) core/settings-manager.ts).
func TestSettingsProjectNestedNullMasksGlobalImageSetting(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"), `{"images":{"autoResize":false,"blockImages":false}}`)
	writeSettingsFile(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"images":{"autoResize":null,"blockImages":true}}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	if !manager.GetImageAutoResize() || !manager.GetBlockImages() {
		t.Fatalf("image settings autoResize=%v blockImages=%v; want defaults/sibling override true/true", manager.GetImageAutoResize(), manager.GetBlockImages())
	}
}

func TestSettingsProjectOrdinaryCompactionNullIsInvalid(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"), `{"compaction":{"reserveTokens":500}}`)
	writeSettingsFile(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"compaction":{"reserveTokens":null}}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	_, err := manager.GetCompactionReserveTokens(nil)
	if err == nil || !strings.Contains(err.Error(), "Invalid compaction.reserveTokens setting: null") {
		t.Fatalf("ordinary reserve-token null error = %v; want upstream invalid-null error", err)
	}
}

// pi v1.4.0 (4a080252) core/settings-manager.ts validates nested model
// overrides and reports null token values as invalid, not as absent overrides.
func TestSettingsProjectModelOverrideNullIsInvalid(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"), `{"compaction":{"reserveTokens":1000,"modelOverrides":{"anthropic/model":{"reserveTokens":200}}}}`)
	writeSettingsFile(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"compaction":{"modelOverrides":{"anthropic/model":{"reserveTokens":null}}}}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	_, err := manager.GetCompactionReserveTokens(&ai.Model{Provider: "anthropic", ID: "model"})
	if err == nil || !strings.Contains(err.Error(), `modelOverrides["anthropic/model"].reserveTokens setting: null`) {
		t.Fatalf("model reserve-token null error = %v; want the upstream invalid-null error", err)
	}
}

// A null map entry remains an invalid non-object model override, per pi v1.4.0
// (4a080252) core/settings-manager.ts getCompactionTokenSetting.
func TestSettingsProjectModelOverrideEntryNullIsInvalid(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"), `{"compaction":{"modelOverrides":{"anthropic/model":{"reserveTokens":200}}}}`)
	writeSettingsFile(t, filepath.Join(projectDir, ConfigDirName, "settings.json"), `{"compaction":{"modelOverrides":{"anthropic/model":null}}}`)

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	_, err := manager.GetCompactionReserveTokens(&ai.Model{Provider: "anthropic", ID: "model"})
	if err == nil || !strings.Contains(err.Error(), `modelOverrides["anthropic/model"] setting: null`) {
		t.Fatalf("model override entry null error = %v; want upstream invalid-null error", err)
	}
}

func TestSettingsMigrations(t *testing.T) {
	// queueMode -> steeringMode
	migrated := MigrateSettingsRaw(map[string]any{"queueMode": "all"})
	if migrated.SteeringMode == nil || *migrated.SteeringMode != "all" {
		t.Fatalf("steeringMode = %v", migrated.SteeringMode)
	}
	// websockets -> transport
	migrated = MigrateSettingsRaw(map[string]any{"websockets": true})
	if migrated.Transport == nil || *migrated.Transport != "websocket" {
		t.Fatalf("transport = %v", migrated.Transport)
	}
	migrated = MigrateSettingsRaw(map[string]any{"websockets": false})
	if migrated.Transport == nil || *migrated.Transport != "sse" {
		t.Fatalf("transport = %v", migrated.Transport)
	}
	// skills object -> array (+ enableSkillCommands)
	migrated = MigrateSettingsRaw(map[string]any{
		"skills": map[string]any{"enableSkillCommands": false, "customDirectories": []any{"/skills/a"}},
	})
	if migrated.EnableSkillCommands == nil || *migrated.EnableSkillCommands {
		t.Fatalf("enableSkillCommands = %v", migrated.EnableSkillCommands)
	}
	if len(migrated.Skills) != 1 || migrated.Skills[0] != "/skills/a" {
		t.Fatalf("skills = %#v", migrated.Skills)
	}
	migrated = MigrateSettingsRaw(map[string]any{"skills": map[string]any{"customDirectories": []any{}}})
	if migrated.Skills != nil {
		t.Fatalf("skills = %#v", migrated.Skills)
	}
	// retry.maxDelayMs -> retry.provider.maxRetryDelayMs
	migrated = MigrateSettingsRaw(map[string]any{"retry": map[string]any{"maxDelayMs": float64(1234)}})
	if migrated.Retry == nil || migrated.Retry.Provider == nil || migrated.Retry.Provider.MaxRetryDelayMS == nil ||
		*migrated.Retry.Provider.MaxRetryDelayMS != 1234 {
		t.Fatalf("retry = %+v", migrated.Retry)
	}
	if raw := settingsToMap(migrated); raw["retry"].(map[string]any)["maxDelayMs"] != nil {
		t.Fatalf("legacy key survived: %#v", raw["retry"])
	}
}

func TestSettingsDeepMerge(t *testing.T) {
	enabled := false
	base := &Settings{
		Compaction: &SettingsCompaction{Enabled: &enabled, ReserveTokens: new(int64(100))},
		Extensions: []string{"/a"},
	}
	keepRecent := int64(50)
	overrides := &Settings{
		Compaction: &SettingsCompaction{KeepRecentTokens: &keepRecent},
		Extensions: []string{"/b", "/c"},
	}
	merged := DeepMergeSettings(base, overrides)
	if merged.Compaction == nil || merged.Compaction.Enabled == nil || *merged.Compaction.Enabled {
		t.Fatalf("merged compaction = %+v", merged.Compaction)
	}
	if merged.Compaction.ReserveTokens == nil || *merged.Compaction.ReserveTokens != 100 {
		t.Fatalf("reserveTokens = %v", merged.Compaction.ReserveTokens)
	}
	if merged.Compaction.KeepRecentTokens == nil || *merged.Compaction.KeepRecentTokens != 50 {
		t.Fatalf("keepRecentTokens = %v", merged.Compaction.KeepRecentTokens)
	}
	// Arrays replace rather than merge.
	if len(merged.Extensions) != 2 || merged.Extensions[0] != "/b" {
		t.Fatalf("extensions = %#v", merged.Extensions)
	}
}
