package coding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// sessionWithResources builds a session the way the CLI does, over temp dirs.
func sessionWithResources(t *testing.T, settings *SettingsManager, options *CreateAgentSessionOptions) *AgentSession {
	t.Helper()
	options.Cwd = t.TempDir()
	options.ModelRuntime = testSessionRuntime(t)
	options.SettingsManager = settings
	created, err := CreateAgentSession(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return created.Session
}

func skillNamesOf(session *AgentSession) []string {
	names := make([]string, 0, len(session.Skills()))
	for _, skill := range session.Skills() {
		names = append(names, skill.Name)
	}
	return names
}

func templateNamesOf(session *AgentSession) []string {
	names := make([]string, 0, len(session.PromptTemplates()))
	for _, template := range session.PromptTemplates() {
		names = append(names, template.Name)
	}
	return names
}

// --prompt-template paths reach the session, which is what makes the /template
// expansion and the prompt commands work at all.
func TestCreateAgentSessionPromptTemplates(t *testing.T) {
	tempAgentDir(t)
	dir := t.TempDir()
	writeSettingsFile(t, filepath.Join(dir, "greet.md"), "---\ndescription: Greeting\n---\nHello $1")

	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session := sessionWithResources(t, settings, &CreateAgentSessionOptions{
		PromptTemplatePaths: []string{dir},
	})
	if names := templateNamesOf(session); len(names) != 1 || names[0] != "greet" {
		t.Errorf("templates = %v, want [greet]", names)
	}

	// An explicit path is still honored when discovery is off.
	session = sessionWithResources(t, settings, &CreateAgentSessionOptions{
		PromptTemplatePaths: []string{dir}, NoPromptTemplates: true,
	})
	if names := templateNamesOf(session); len(names) != 1 || names[0] != "greet" {
		t.Errorf("templates with NoPromptTemplates = %v, want [greet]", names)
	}

	// ...and with nothing explicit, discovery off really means none.
	if names := templateNamesOf(sessionWithResources(t, settings, &CreateAgentSessionOptions{
		NoPromptTemplates: true,
	})); len(names) != 0 {
		t.Errorf("templates with NoPromptTemplates alone = %v, want none", names)
	}
}

// TestPromptTemplateCollisionsReachSession follows v1.2.0 resource-loader.ts
// dedupePrompts: first name wins and the loser is reported, including reload.
func TestPromptTemplateCollisionsReachSession(t *testing.T) {
	tempAgentDir(t)
	first, second := t.TempDir(), t.TempDir()
	winner, loser := filepath.Join(first, "greet.md"), filepath.Join(second, "greet.md")
	writeSettingsFile(t, winner, "winner")
	writeSettingsFile(t, loser, "loser")
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session := sessionWithResources(t, settings, &CreateAgentSessionOptions{
		PromptTemplatePaths: []string{first, second}, NoPromptTemplates: true,
	})
	check := func() {
		t.Helper()
		templates := session.PromptTemplates()
		if len(templates) != 1 || templates[0].FilePath != winner {
			t.Fatalf("templates = %#v, want only first /greet", templates)
		}
		loader, ok := any(session).(interface{ PromptDiagnostics() []ResourceDiagnostic })
		if !ok {
			t.Fatal("session does not expose prompt diagnostics")
		}
		diagnostics := loader.PromptDiagnostics()
		if len(diagnostics) != 1 || diagnostics[0].Type != "collision" || diagnostics[0].Message != `name "/greet" collision` {
			t.Fatalf("diagnostics = %#v", diagnostics)
		}
		collision := diagnostics[0].Collision
		if collision == nil || collision.ResourceType != "prompt" || collision.WinnerPath != winner || collision.LoserPath != loser {
			t.Fatalf("collision = %#v", collision)
		}
	}
	check()
	session.Reload()
	check()
}

// TestUnreadablePromptDiagnostic follows prompt-templates.ts read warnings.
func TestUnreadablePromptDiagnostic(t *testing.T) {
	tempAgentDir(t)
	path := filepath.Join(t.TempDir(), "denied.md")
	writeSettingsFile(t, path, "unreadable")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("filesystem does not enforce file read permissions")
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session := sessionWithResources(t, settings, &CreateAgentSessionOptions{
		PromptTemplatePaths: []string{path}, NoPromptTemplates: true,
	})
	diagnostics := session.PromptDiagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Type != "warning" || diagnostics[0].Path != path || diagnostics[0].Message == "" {
		t.Fatalf("unreadable prompt diagnostics = %#v", diagnostics)
	}
}

// Reload is a re-read, not a reset: the CLI switches have to survive it, or
// --no-skills would come back the first time a user reloaded and an explicit
// --skill path would vanish.
func TestReloadKeepsCLIResourceSwitches(t *testing.T) {
	tempAgentDir(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})

	settingsSkillDir := t.TempDir()
	writeSkill(t, settingsSkillDir, "from-settings", "discovered through settings")
	settings.SetSkillPaths([]string{settingsSkillDir})

	explicitSkillDir := t.TempDir()
	writeSkill(t, explicitSkillDir, "explicit", "named on the command line")

	templateDir := t.TempDir()
	writeSettingsFile(t, filepath.Join(templateDir, "kept.md"), "---\ndescription: Kept\n---\nBody")

	session := sessionWithResources(t, settings, &CreateAgentSessionOptions{
		SkillPaths:          []string{explicitSkillDir},
		NoSkills:            true,
		PromptTemplatePaths: []string{templateDir},
		NoPromptTemplates:   true,
	})
	if names := skillNamesOf(session); len(names) != 1 || names[0] != "explicit" {
		t.Fatalf("skills before reload = %v, want [explicit]", names)
	}

	session.Reload()

	if names := skillNamesOf(session); len(names) != 1 || names[0] != "explicit" {
		t.Errorf("skills after reload = %v, want [explicit] — the switches were reset", names)
	}
	if names := templateNamesOf(session); len(names) != 1 || names[0] != "kept" {
		t.Errorf("templates after reload = %v, want [kept]", names)
	}
}

// The switches being off is also preserved: a reload must not resurrect
// discovery that was suppressed.
func TestReloadKeepsSuppression(t *testing.T) {
	tempAgentDir(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	settingsSkillDir := t.TempDir()
	writeSkill(t, settingsSkillDir, "from-settings", "discovered through settings")
	settings.SetSkillPaths([]string{settingsSkillDir})

	cwd := t.TempDir()
	writeSettingsFile(t, filepath.Join(cwd, "AGENTS.md"), "project rules\n")

	session := sessionWithResources(t, settings, &CreateAgentSessionOptions{
		NoSkills:       true,
		NoContextFiles: true,
	})
	session.Cwd = cwd
	session.Reload()

	if names := skillNamesOf(session); len(names) != 0 {
		t.Errorf("skills after reload = %v, want none (--no-skills was undone)", names)
	}
	if files := session.ContextFiles(); len(files) != 0 {
		t.Errorf("context files after reload = %d, want none (--no-context-files was undone)", len(files))
	}
	// The project file is there to be found, so the suppression is what hid it.
	if _, err := os.Stat(filepath.Join(cwd, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
}
