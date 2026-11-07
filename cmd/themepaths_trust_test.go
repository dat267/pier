package cmd

import (
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
// the project scope (M249), so themePathsFor must gate the list on trust
// itself: pre-trust the global scope's values, post-trust the merged ones
// (upstream deep-merge replaces arrays, so a project `themes` replaces the
// global one there).
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
