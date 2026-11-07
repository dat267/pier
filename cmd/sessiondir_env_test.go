package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// Reference: pi v1.5.0 main.ts resolves the session directory as
// `--session-dir` ?? $PI_CODING_AGENT_SESSION_DIR (tilde-expanded) ??
// settings.getSessionDir(). Pier read only `--session-dir`, so a run with the
// environment variable or the setting configured wrote its session into the
// default per-cwd agent directory, and `-c`/`-r` searched the wrong place.
func TestResolveSessionDirectoryMatchesTheReferencePrecedence(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")

	settingsWith := func(body string) *coding.SettingsManager {
		t.Helper()
		if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return coding.NewSettingsManagerFromFiles(cwd, agentDir, coding.SettingsManagerCreateOptions{})
	}

	explicit := filepath.Join(t.TempDir(), "explicit")
	fromEnv := filepath.Join(t.TempDir(), "from-env")
	fromSettings := filepath.Join(t.TempDir(), "from-settings")

	// Settings only.
	settings := settingsWith(`{"sessionDir": "` + fromSettings + `"}`)
	if got := resolveSessionDirectory(nil, settings); got != fromSettings {
		t.Errorf("settings sessionDir = %q, want %q", got, fromSettings)
	}

	// The environment variable outranks the setting.
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", fromEnv)
	if got := resolveSessionDirectory(nil, settings); got != fromEnv {
		t.Errorf("env sessionDir = %q, want %q (env outranks settings)", got, fromEnv)
	}

	// --session-dir outranks both.
	flag := explicit
	if got := resolveSessionDirectory(&coding.Args{SessionDir: &flag}, settings); got != explicit {
		t.Errorf("--session-dir = %q, want %q (flag outranks env and settings)", got, explicit)
	}

	// A tilde in the environment value expands, like expandTildePath.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "~/sessions")
	if got, want := resolveSessionDirectory(nil, settings), filepath.Join(os.Getenv("HOME"), "sessions"); got != want {
		t.Errorf("tilde env sessionDir = %q, want %q", got, want)
	}

	// With nothing configured the empty string keeps the per-cwd default.
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	if got := resolveSessionDirectory(nil, settingsWith(`{}`)); got != "" {
		t.Errorf("unconfigured sessionDir = %q, want the per-cwd default", got)
	}
}

// resumeSession must use the resolved directory for --session-id creation.
func TestResumeSessionIDUsesTheConfiguredSessionDir(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	dir := filepath.Join(t.TempDir(), "configured")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", dir)
	settings := coding.NewSettingsManagerFromFiles(cwd, agentDir, coding.SettingsManagerCreateOptions{})
	id := "019463a7-2222-7222-8222-222222222222"
	args := &coding.Args{SessionID: &id}
	manager, err := resumeSession(args, cwd, agentDir, settings)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.GetSessionDir(); got != dir {
		t.Errorf("session dir = %q, want %q", got, dir)
	}
	if got := manager.GetSessionFile(); filepath.Dir(got) != dir {
		t.Errorf("session file %q is not under %q", got, dir)
	}
}

// A fresh (non-resume) run must also honor the configured directory.
func TestFreshSessionUsesTheConfiguredSessionDir(t *testing.T) {
	agentDir := t.TempDir()
	dir := filepath.Join(t.TempDir(), "configured")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", dir)
	settings := coding.NewSettingsManagerFromFiles(t.TempDir(), agentDir, coding.SettingsManagerCreateOptions{})
	options := &coding.SessionManagerOptions{SessionDir: resolveSessionDirectory(&coding.Args{}, settings)}
	manager := coding.NewSessionManager(t.TempDir(), options)
	if got := manager.GetSessionDir(); got != dir {
		t.Errorf("fresh session dir = %q, want %q", got, dir)
	}
}

// Reference: pi v1.5.0 main.ts builds `startupSettingsManager` with
// `SettingsManager.create(cwd, agentDir)`, whose project scope defaults to
// trusted (core/settings-manager.ts `options.projectTrusted ?? true`). A
// project `.pi/settings.json` sessionDir therefore picks the startup session
// directory, an unreadable project file joins the startup diagnostics, and
// defaultProjectTrust still comes only from the global settings
// (`getDefaultProjectTrust` reads globalSettings), so a project cannot trust
// itself.
func TestStartupSettingsReadTheProjectScopeLikeTheReference(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	writeProject := func(cwd, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(cwd, ".pi"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".pi", "settings.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cwd := t.TempDir()
	fromProject := filepath.Join(t.TempDir(), "from-project")
	writeProject(cwd, `{"sessionDir": "`+fromProject+`", "defaultProjectTrust": "always"}`)
	settings := newStartupSettingsManager(cwd, agentDir)
	if got := resolveSessionDirectory(nil, settings); got != fromProject {
		t.Errorf("startup sessionDir = %q, want the project's %q", got, fromProject)
	}
	if got := settings.GetDefaultProjectTrust(); got != "ask" {
		t.Errorf("defaultProjectTrust = %q, want the global default ask (a project cannot trust itself)", got)
	}
	if diagnostics := coding.CollectSettingsDiagnostics(settings); len(diagnostics) != 0 {
		t.Errorf("valid project settings reported %v", diagnostics)
	}

	broken := t.TempDir()
	writeProject(broken, `{not json`)
	diagnostics := coding.CollectSettingsDiagnostics(newStartupSettingsManager(broken, agentDir))
	if len(diagnostics) != 1 || diagnostics[0].Type != "warning" ||
		!strings.Contains(diagnostics[0].Message, filepath.Join(broken, ".pi", "settings.json")) {
		t.Errorf("broken project settings diagnostics = %+v, want one project warning", diagnostics)
	}
}
