package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/coding/interactive"
)

// Reference: pi v1.5.0 (f9a26365) cli/startup-ui.ts loadStartupThemes resolves
// the pre-trust theme paths through
// SettingsManager.inMemory(settingsManager.getGlobalSettings(), {
// projectTrusted: false }), and core/package-manager.ts collects project
// entries only `if (projectTrusted)` (collectAutoResourceEntries). An untrusted
// project's settings `.themes` values therefore name no files before the trust
// decision; the theme setting itself still comes from the merged manager,
// because createStartupTui reads settingsManager.getThemeSetting(). The CLI
// --theme values are absent there too: they flow into the trusted runtime's
// resource loader, and pier follows that order. Pier's startup manager reads
// the project scope (M249), so themePathsFor gates the list on trust itself:
// pre-trust it uses the global scope; post-trust it concatenates project paths,
// agent paths and --theme paths because upstream package-manager.ts resolves
// both settings scopes as resources.
func TestThemePathsPreTrustReadTheGlobalScopeOnly(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	dark, err := os.ReadFile("../coding/interactive/themes/dark.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectTheme := filepath.Join(cwd, ".pi", "project.json")
	globalTheme := filepath.Join(agentDir, "global.json")
	write := func(path, name string) {
		t.Helper()
		if err := os.WriteFile(path,
			[]byte(strings.Replace(string(dark), `"name": "dark"`, `"name": "`+name+`"`, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(projectTheme, "project")
	write(globalTheme, "globalized")
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"),
		[]byte(`{"themes": ["`+globalTheme+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pi", "settings.json"),
		[]byte(`{"themes": ["`+projectTheme+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	settings := newStartupSettingsManager(cwd, agentDir)
	args := &coding.Args{Themes: []string{"cli.json"}}
	pre := themePathsFor(args, settings, cwd, false)
	if got, want := pre, []string{globalTheme}; !slices.Equal(got, want) {
		t.Errorf("pre-trust theme paths = %v, want the global scope only", got)
	}
	// Post-trust, the project scope and the agent scope both load (upstream
	// package-manager.ts resolve adds project overrides only when trusted and
	// user overrides always, into one path map), then the CLI paths
	// (resource-loader.ts mergePaths([...cliEnabledThemes, ...enabledThemes],
	// additionalThemePaths)).
	post := themePathsFor(args, settings, cwd, true)
	if got, want := post, []string{
		filepath.Join(cwd, ".pi", "project.json"),
		globalTheme,
		filepath.Join(cwd, "cli.json"),
	}; !slices.Equal(got, want) {
		t.Errorf("post-trust theme paths = %v, want the project scope, the global scope and the CLI flag", got)
	}

	// End to end: the named project file is not read or registered pre-trust.
	boot := interactive.NewThemeBoot()
	boot.EnableCapabilities()
	boot.ApplySources(interactive.ThemeSources{
		AgentDir:   agentDir,
		Cwd:        cwd,
		ThemeName:  themeNameFor(settings),
		ThemePaths: pre,
		Trusted:    false,
	})
	names := interactive.AvailableThemes()
	if slices.Contains(names, "project") {
		t.Errorf("the project scope's theme registered pre-trust: %v", names)
	}
	if !slices.Contains(names, "globalized") {
		t.Errorf("the global scope's theme missing pre-trust: %v", names)
	}

	// Post-trust both scopes register: the project's file and the agent's
	// (A398; upstream package-manager.ts resolve adds both to one path map).
	boot.ApplySources(interactive.ThemeSources{
		AgentDir:   agentDir,
		Cwd:        cwd,
		ThemeName:  themeNameFor(settings),
		ThemePaths: post,
		Trusted:    true,
	})
	names = interactive.AvailableThemes()
	for _, want := range []string{"project", "globalized"} {
		if !slices.Contains(names, want) {
			t.Errorf("post-trust themes missing %q: %v", want, names)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) core/resource-loader.ts mergePaths uses
// getResourcePathKey before loadThemes, so the same theme path from both scopes
// is loaded once and does not create a name-collision diagnostic.
func TestThemePathsDeduplicateAcrossScopes(t *testing.T) {
	agentDir, cwd := t.TempDir(), t.TempDir()
	dark, err := os.ReadFile("../coding/interactive/themes/dark.json")
	if err != nil {
		t.Fatal(err)
	}
	themePath := filepath.Join(t.TempDir(), "shared.json")
	content := strings.Replace(string(dark), `"name": "dark"`, `"name": "scope-shared"`, 1)
	if err := os.WriteFile(themePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := json.Marshal([]string{themePath})
	if err != nil {
		t.Fatal(err)
	}
	for _, settingsPath := range []string{
		filepath.Join(agentDir, "settings.json"),
		filepath.Join(cwd, ".pi", "settings.json"),
	} {
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			t.Fatal(err)
		}
		config := []byte(`{"themes":` + string(paths) + `}`)
		if err := os.WriteFile(settingsPath, config, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	settings := newStartupSettingsManager(cwd, agentDir)
	gotPaths := themePathsFor(&coding.Args{}, settings, cwd, true)
	if len(gotPaths) != 1 {
		t.Errorf("theme paths = %v, want one canonical path", gotPaths)
	}
	boot := interactive.NewThemeBoot()
	boot.EnableCapabilities()
	boot.ApplySources(interactive.ThemeSources{AgentDir: agentDir, Cwd: cwd, ThemePaths: gotPaths, Trusted: true})
	for _, diagnostic := range interactive.ThemeDiagnostics() {
		if diagnostic.Type == "collision" {
			t.Errorf("same theme file produced a collision: %+v", diagnostic)
		}
	}
}
