package coding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/internal/offloop"
)

// D160 keeps one manager across session switches. Like creating a fresh
// manager in pi v1.4.0 settings-manager.ts, a failed project load must record
// the error rather than block the switch. Re-exec bounds the old mutex deadlock.
func TestSettingsRebindReportsMalformedProjectSettings(t *testing.T) {
	const probeEnv = "PIER_TEST_SETTINGS_REBIND_ERROR"
	if os.Getenv(probeEnv) == "1" {
		agentDir, projectDir := settingsDirs(t)
		manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
		other := t.TempDir()
		path := filepath.Join(other, ConfigDirName, "settings.json")
		writeSettingsFile(t, path, `{"theme":`)

		manager.RebindProject(other, true)
		failures := manager.DrainErrors()
		if len(failures) != 1 || failures[0].Scope != SettingsScopeProject || failures[0].Path != path || failures[0].Error == nil {
			t.Fatalf("project load failures = %+v", failures)
		}
		if manager.Cwd() != NormalizePath(other, PathInputOptions{}) {
			t.Fatalf("project not rebound: %q", manager.Cwd())
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSettingsRebindReportsMalformedProjectSettings$", "-test.timeout=4s")
	command.Env = append(os.Environ(), probeEnv+"=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("rebinding malformed project settings did not return with diagnostics: %v\n%s", err, output)
	}
}

// pi v1.4.0 main.ts creates a separate cwd-bound settings manager on session
// replacement. D160 reuses one Go manager, but accepted writes still belong to
// the project in which the user submitted them.
func TestQueuedProjectSettingsKeepTheirDestinationAcrossRebind(t *testing.T) {
	agentDir := t.TempDir()
	cwdA, cwdB := t.TempDir(), t.TempDir()
	writeProjectSettings(t, cwdA, "alpha-theme")
	writeProjectSettings(t, cwdB, "beta-theme")
	queue := offloop.New()
	manager := NewSettingsManagerFromFiles(cwdA, agentDir, SettingsManagerCreateOptions{PersistQueue: queue})
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		queue.Stop()
	}()
	queue.Go(func() { close(started); <-release })
	<-started
	manager.SetProjectThemePaths([]string{"project-a-only"})
	manager.RebindProject(cwdB, true)
	close(release)
	manager.FlushPersists()

	savedA := readSettingsFile(t, filepath.Join(cwdA, ConfigDirName, "settings.json"))
	savedB := readSettingsFile(t, filepath.Join(cwdB, ConfigDirName, "settings.json"))
	if paths, ok := savedA["themes"].([]any); !ok || len(paths) != 1 || paths[0] != "project-a-only" {
		t.Errorf("accepted write did not reach project A: %#v", savedA)
	}
	if _, present := savedB["themes"]; present {
		t.Errorf("project A settings leaked into project B: %#v", savedB)
	}
	if savedA["theme"] != "alpha-theme" || savedB["theme"] != "beta-theme" {
		t.Errorf("unmodified project settings changed: A=%#v B=%#v", savedA, savedB)
	}
}

// D160: a queued write failure must name the project that accepted the save,
// not the project selected when the worker finishes.
func TestQueuedProjectSettingsErrorKeepsItsPathAcrossRebind(t *testing.T) {
	agentDir := t.TempDir()
	cwdA, cwdB := t.TempDir(), t.TempDir()
	writeProjectSettings(t, cwdA, "alpha-theme")
	writeProjectSettings(t, cwdB, "beta-theme")
	queue := offloop.New()
	manager := NewSettingsManagerFromFiles(cwdA, agentDir, SettingsManagerCreateOptions{PersistQueue: queue})
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		queue.Stop()
	}()
	queue.Go(func() { close(started); <-release })
	<-started
	manager.SetProjectThemePaths([]string{"project-a-only"})
	pathA := filepath.Join(cwdA, ConfigDirName, "settings.json")
	if err := os.Remove(pathA); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pathA, 0o755); err != nil {
		t.Fatal(err)
	}
	manager.RebindProject(cwdB, true)
	close(release)
	manager.FlushPersists()

	failures := manager.DrainErrors()
	if len(failures) != 1 || failures[0].Scope != SettingsScopeProject || failures[0].Path != pathA {
		t.Fatalf("failure was not attributed to original project %q: %+v", pathA, failures)
	}
}

// D160/D280: completing an old project's save must not acknowledge equal
// values in a new project's dirty set; that new project's own save can fail.
func TestOldProjectSaveDoesNotClearNewProjectRetry(t *testing.T) {
	agentDir := t.TempDir()
	cwdA, cwdB := t.TempDir(), t.TempDir()
	writeProjectSettings(t, cwdA, "alpha-theme")
	writeProjectSettings(t, cwdB, "beta-theme")
	queue := offloop.New()
	manager := NewSettingsManagerFromFiles(cwdA, agentDir, SettingsManagerCreateOptions{PersistQueue: queue})
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		queue.Stop()
	}()
	queue.Go(func() { close(started); <-release })
	<-started
	manager.SetProjectThemePaths([]string{"shared-name"})
	manager.RebindProject(cwdB, true)
	manager.SetProjectThemePaths([]string{"shared-name"})

	pathB := filepath.Join(cwdB, ConfigDirName, "settings.json")
	if err := os.Remove(pathB); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pathB, 0o755); err != nil {
		t.Fatal(err)
	}
	close(release)
	manager.FlushPersists()
	if failures := manager.DrainErrors(); len(failures) != 1 || failures[0].Path != pathB {
		t.Fatalf("project B write failures = %+v", failures)
	}
	if err := os.Remove(pathB); err != nil {
		t.Fatal(err)
	}
	writeProjectSettings(t, cwdB, "beta-theme")
	manager.SetProjectSkillPaths([]string{"retry-trigger"})
	manager.FlushPersists()

	saved := readSettingsFile(t, pathB)
	if paths, ok := saved["themes"].([]any); !ok || len(paths) != 1 || paths[0] != "shared-name" {
		t.Fatalf("old project completion cleared new project's failed setting: %#v", saved)
	}
}

// writeProjectSettings writes a project settings file with one theme value.
func writeProjectSettings(t *testing.T, cwd, theme string) {
	t.Helper()
	dir := filepath.Join(cwd, ConfigDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"theme":"` + theme + `"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A session switch can land in another directory, and the runtime is cwd-bound
// upstream: createRuntime builds a settings manager for the session's cwd and
// resolves that cwd's trust. This port keeps one manager per process, so the
// project half is re-pointed instead.
func TestSettingsManagerRebindProject(t *testing.T) {
	agentDir := t.TempDir()
	globalPath := filepath.Join(agentDir, "settings.json")
	if err := os.WriteFile(globalPath, []byte(`{"defaultModel":"global-model"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cwdA, cwdB := t.TempDir(), t.TempDir()
	writeProjectSettings(t, cwdA, "alpha-theme")
	writeProjectSettings(t, cwdB, "beta-theme")

	manager := NewSettingsManagerFromFiles(cwdA, agentDir, SettingsManagerCreateOptions{})
	if manager.Cwd() != NormalizePath(cwdA, PathInputOptions{}) {
		t.Fatalf("cwd = %q", manager.Cwd())
	}
	if theme := manager.GetTheme(); theme == nil || *theme != "alpha-theme" {
		t.Fatalf("theme = %v", theme)
	}

	// Re-pointed at B: B's project settings apply, A's are gone, and the global
	// scope is untouched.
	manager.RebindProject(cwdB, true)
	if manager.Cwd() != NormalizePath(cwdB, PathInputOptions{}) {
		t.Fatalf("cwd = %q", manager.Cwd())
	}
	if theme := manager.GetTheme(); theme == nil || *theme != "beta-theme" {
		t.Fatalf("theme = %v", theme)
	}
	if model := manager.GetDefaultModel(); model == nil || *model != "global-model" {
		t.Fatalf("global settings lost: defaultModel = %v", model)
	}
	if !manager.IsProjectTrusted() {
		t.Fatal("trusted project must stay trusted")
	}

	// Untrusted: the project half is dropped, and there is no trust decision for
	// it in the manager.
	manager.RebindProject(cwdA, false)
	if manager.IsProjectTrusted() {
		t.Fatal("rebind must carry the new trust decision")
	}
	if theme := manager.GetTheme(); theme != nil {
		t.Fatalf("untrusted project settings were read: %v", *theme)
	}
	if model := manager.GetDefaultModel(); model == nil || *model != "global-model" {
		t.Fatalf("global settings lost: defaultModel = %v", model)
	}

	// A trusted rebind back to a project with no settings file is fine, and
	// writes now target that cwd's file.
	empty := t.TempDir()
	manager.RebindProject(empty, true)
	manager.SetProjectThemePaths([]string{"written-theme-path"})
	raw, err := os.ReadFile(filepath.Join(empty, ConfigDirName, "settings.json"))
	if err != nil {
		t.Fatalf("write went to the wrong project: %v", err)
	}
	if !strings.Contains(string(raw), "written-theme-path") {
		t.Fatalf("settings file = %s", raw)
	}
}
