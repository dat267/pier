package interactive

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dat267/pier/coding"
	"testing"
)

// TestLoadedPromptConflictsFollowsQuietStartup follows v1.2.0
// interactive-mode.ts showLoadedResources: diagnostics remain visible when
// startup listings are quiet, and prompt collisions include both paths.
func TestLoadedPromptConflictsFollowsQuietStartup(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(dir, "greet.md"), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app.options.Settings.SetPromptTemplatePaths([]string{first, second})
	app.session.Reload()
	app.showLoadedResources(false)
	output := coding.StripAnsi(strings.Join(app.loadedResourcesContainer.Render(160), "\n"))
	for _, want := range []string{"[Prompt conflicts]", `"greet" collision:`, "✓ path (temp) " + filepath.Join(first, "greet.md"), second} {
		if !strings.Contains(output, want) {
			t.Errorf("loaded diagnostics = %q, missing %q", output, want)
		}
	}
	if strings.Contains(output, "[Prompts]") {
		t.Fatal("quiet startup showed a prompt listing")
	}
}

// TestLoadedResourcesHasNoThemesSection follows v1.2.0 showLoadedResources:
// themes contribute diagnostic metadata, not a separate startup listing.
func TestLoadedResourcesHasNoThemesSection(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.showLoadedResources(true)
	output := coding.StripAnsi(strings.Join(app.loadedResourcesContainer.Render(160), "\n"))
	if strings.Contains(output, "[Themes]") {
		t.Fatalf("unexpected upstream-absent Themes section: %q", output)
	}
}

// TestLoadedThemeWarnings follows resource-loader.ts theme path warnings and
// interactive-mode.ts diagnostics, including quiet startup.
func TestLoadedThemeWarnings(t *testing.T) {
	for _, name := range []string{"invalid.json", "missing.json", "notes.txt"} {
		t.Run(name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			path := filepath.Join(t.TempDir(), name)
			if name != "missing.json" {
				if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			SetCustomThemeSources(CustomThemeSources{Paths: []string{path}, NoDiscovery: true})
			app.showLoadedResources(false)
			output := coding.StripAnsi(strings.Join(app.loadedResourcesContainer.Render(160), "\n"))
			if !strings.Contains(output, "[Theme conflicts]") || !strings.Contains(output, path) {
				t.Fatalf("theme warnings = %q, want conflict section with %s", output, path)
			}
			SetCustomThemeSources(CustomThemeSources{NoDiscovery: true})
			app.showLoadedResources(false)
			if output := strings.Join(app.loadedResourcesContainer.Render(160), "\n"); strings.Contains(output, "[Theme conflicts]") {
				t.Fatalf("old source warnings survived replacement: %q", output)
			}
		})
	}
}

// TestThemeCollisionsKeepFirstSource follows resource-loader.ts loadThemes and
// dedupeThemes: defaults precede explicit paths, and duplicates are diagnosed.
func TestThemeCollisionsKeepFirstSource(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeThemeFile(t, first, "shared.json", "shared", "#ff0000")
	writeThemeFile(t, second, "shared.json", "shared", "#00ff00")
	SetCustomThemeSources(CustomThemeSources{Dir: first, Paths: []string{second}})
	var found []ThemeInfo
	for _, info := range AvailableThemesWithPaths() {
		if info.Name == "shared" {
			found = append(found, info)
		}
	}
	winner, loser := filepath.Join(first, "shared.json"), filepath.Join(second, "shared.json")
	if len(found) != 1 || found[0].Path != winner {
		t.Errorf("theme lookup = %#v, want %s", found, winner)
	}
	diagnostics := ThemeDiagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Type != "collision" {
		t.Fatalf("theme diagnostics = %#v", diagnostics)
	}
	collision := diagnostics[0].Collision
	if collision == nil || collision.ResourceType != "theme" || collision.WinnerPath != winner || collision.LoserPath != loser {
		t.Fatalf("theme collision = %#v", collision)
	}
}

// TestDiscoveredThemeScopes follows resource-loader.ts synthetic SourceInfo.
func TestDiscoveredThemeScopes(t *testing.T) {
	agentDir, project, explicit := t.TempDir(), t.TempDir(), t.TempDir()
	userThemes := filepath.Join(agentDir, "themes")
	projectThemes := filepath.Join(project, coding.ConfigDirName, "themes")
	for _, dir := range []string{userThemes, projectThemes} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeThemeFile(t, userThemes, "user.json", "user-theme", "#ff0000")
	writeThemeFile(t, projectThemes, "project.json", "project-theme", "#00ff00")
	writeThemeFile(t, explicit, "path.json", "path-theme", "#0000ff")
	boot := NewThemeBoot()
	boot.EnableCapabilities()
	boot.ApplySources(ThemeSources{AgentDir: agentDir, Cwd: project, Trusted: true, ThemePaths: []string{explicit}})
	byName := map[string]ThemeInfo{}
	for _, info := range themeResourceInfos() {
		byName[info.Name] = info
	}
	for name, want := range map[string]string{
		"user-theme": "user ", "project-theme": "project ", "path-theme": "path (temp) ",
	} {
		info, ok := byName[name]
		if !ok {
			t.Fatalf("theme %q missing from %#v", name, byName)
		}
		label := coding.StripAnsi(formatPathWithSource(info.Path, map[string]coding.SourceInfo{info.Path: info.SourceInfo}))
		if !strings.HasPrefix(label, want) {
			t.Errorf("theme %q label = %q, want prefix %q", name, label, want)
		}
	}
}
