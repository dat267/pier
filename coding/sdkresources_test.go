package coding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --skill paths load even under --no-skills: refusing discovery is not refusing
// what was asked for explicitly (upstream resourceLoader keeps its
// additionalSkillPaths when noSkills is set).
func TestCreateAgentSessionSkillPaths(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	cwd := t.TempDir()
	skillDir := t.TempDir()
	writeSkill(t, skillDir, "extra", "an extra skill")

	skillNames := func(options *CreateAgentSessionOptions) []string {
		t.Helper()
		options.Cwd = cwd
		options.ModelRuntime = runtime
		options.SettingsManager = settings
		created, err := CreateAgentSession(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(created.Session.Skills()))
		for _, skill := range created.Session.Skills() {
			names = append(names, skill.Name)
		}
		return names
	}

	got := skillNames(&CreateAgentSessionOptions{SkillPaths: []string{skillDir}})
	if len(got) != 1 || got[0] != "extra" {
		t.Fatalf("skills = %v, want [extra]", got)
	}

	// Discovery is off, but the explicit path is still honored.
	got = skillNames(&CreateAgentSessionOptions{SkillPaths: []string{skillDir}, NoSkills: true})
	if len(got) != 1 || got[0] != "extra" {
		t.Errorf("skills with NoSkills = %v, want [extra]", got)
	}

	// ...and with nothing explicit, NoSkills really does mean nothing.
	if got := skillNames(&CreateAgentSessionOptions{NoSkills: true}); len(got) != 0 {
		t.Errorf("skills with NoSkills alone = %v, want none", got)
	}
}

func TestCreateAgentSessionNoContextFiles(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("project rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	contextFileCount := func(options *CreateAgentSessionOptions) int {
		t.Helper()
		options.Cwd = cwd
		options.ModelRuntime = runtime
		options.SettingsManager = settings
		created, err := CreateAgentSession(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		return len(created.Session.ContextFiles())
	}

	if count := contextFileCount(&CreateAgentSessionOptions{}); count == 0 {
		t.Fatal("the project AGENTS.md should be discovered by default")
	}
	if count := contextFileCount(&CreateAgentSessionOptions{NoContextFiles: true}); count != 0 {
		t.Errorf("context files = %d, want 0 with NoContextFiles", count)
	}
}

// testSessionRuntime builds a runtime with no network and no stored models,
// which is all the resource-loading paths need.
func testSessionRuntime(t *testing.T) *ModelRuntime {
	t.Helper()
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// TestCreateAgentSessionLoadsAllResourcePasses pins the assembled session
// across the four concurrent discovery passes: skills, context files, prompt
// templates and the system prompt files must arrive with the same content and
// order as when the passes ran sequentially. Context files keep the global
// agent-dir file ahead of the workspace ancestors.
func TestCreateAgentSessionLoadsAllResourcePasses(t *testing.T) {
	tempAgentDir(t)
	agentDir := GetAgentDir()
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	cwd := t.TempDir()

	if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte("global rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("project rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SYSTEM.md"), []byte("base system prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "APPEND_SYSTEM.md"), []byte("appended prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(agentDir, "skills"), "alpha", "alpha skill")
	promptsDir := filepath.Join(agentDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	template := "---\ndescription: greet the user\n---\nHello $1\n"
	if err := os.WriteFile(filepath.Join(promptsDir, "greet.md"), []byte(template), 0o600); err != nil {
		t.Fatal(err)
	}

	created, err := CreateAgentSession(context.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.Session

	if got, want := strings.Join(skillNames(LoadSkillsResult{Skills: session.Skills()}), ","), "alpha"; got != want {
		t.Fatalf("skills = %q, want %q", got, want)
	}

	var contextPaths []string
	for _, file := range session.ContextFiles() {
		contextPaths = append(contextPaths, file.Path)
	}
	if len(contextPaths) != 2 ||
		contextPaths[0] != filepath.Join(agentDir, "AGENTS.md") ||
		contextPaths[1] != filepath.Join(cwd, "AGENTS.md") {
		t.Fatalf("context files = %v", contextPaths)
	}

	var templateNames []string
	for _, promptTemplate := range session.PromptTemplates() {
		templateNames = append(templateNames, promptTemplate.Name)
	}
	if strings.Join(templateNames, ",") != "greet" {
		t.Fatalf("prompt templates = %v", templateNames)
	}

	systemPrompt := session.SystemPrompt()
	if !strings.Contains(systemPrompt, "base system prompt") {
		t.Fatalf("system prompt does not carry SYSTEM.md: %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "appended prompt") {
		t.Fatalf("system prompt does not carry APPEND_SYSTEM.md: %q", systemPrompt)
	}
}
