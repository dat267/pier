package coding

import (
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) core/package-manager.ts resolve() adds the
// project's `skills` and `prompts` overrides only `if (projectTrusted)` and the
// user's always, both into one path map, and core/resource-loader.ts merges the
// CLI paths onto that list. A project list therefore adds to the agent list
// instead of replacing it. Pier read the deep-merged settings, whose
// deepMergeObjects replaces arrays, so a project `skills` hid the agent's.
func TestResourcePathsKeepBothScopes(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	projectSkill := filepath.Join(cwd, "project-skills")
	agentSkill := filepath.Join(agentDir, "agent-skills")
	projectPrompt := filepath.Join(cwd, "project-prompts")
	agentPrompt := filepath.Join(agentDir, "agent-prompts")
	writeSkill(t, projectSkill, "project-skill", "project")
	writeSkill(t, agentSkill, "agent-skill", "agent")
	writeSettingsFile(t, filepath.Join(projectPrompt, "project.md"), "---\ndescription: project\n---\nBody")
	writeSettingsFile(t, filepath.Join(agentPrompt, "agent.md"), "---\ndescription: agent\n---\nBody")
	writeSettingsFile(t, filepath.Join(cwd, ".pi", "settings.json"),
		`{"skills": [`+`"`+projectSkill+`"`+`], "prompts": [`+`"`+projectPrompt+`"`+`]}`)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"),
		`{"skills": [`+`"`+agentSkill+`"`+`], "prompts": [`+`"`+agentPrompt+`"`+`]}`)

	settings := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})
	options := resourceOptions{}

	loaded := options.skills(cwd, agentDir, settings, true)
	names := make([]string, 0, len(loaded.Skills))
	for _, skill := range loaded.Skills {
		names = append(names, skill.Name)
	}
	if len(names) != 2 || names[0] != "project-skill" || names[1] != "agent-skill" {
		t.Errorf("skills = %v, want [project-skill agent-skill]", names)
	}

	templates := options.promptTemplates(cwd, agentDir, settings)
	templateNames := make([]string, 0, len(templates.Templates))
	for _, template := range templates.Templates {
		templateNames = append(templateNames, template.Name)
	}
	if len(templateNames) != 2 || templateNames[0] != "project" || templateNames[1] != "agent" {
		t.Errorf("prompt templates = %v, want [project agent]", templateNames)
	}
}

// TestResourceSettingsPathsPrecedeCLIPaths pins the collision order. Reference:
// pi v1.5.0 (f9a26365) core/resource-loader.ts builds the list as
// mergePaths([...cliEnabledSkills, ...enabledSkills], additionalSkillPaths)
// and the analogous prompt list, so settings paths precede --skill and
// --prompt-template paths; first-occurrence dedupe keeps the settings' resource.
func TestResourceSettingsPathsPrecedeCLIPaths(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	settingsSkills := filepath.Join(cwd, "settings-skills")
	cliSkills := filepath.Join(cwd, "cli-skills")
	writeSkill(t, settingsSkills, "shared", "from settings")
	writeSkill(t, cliSkills, "shared", "from the flag")
	writeSkill(t, cliSkills, "cli-only", "still loaded from the flag")
	settingsPrompts := filepath.Join(cwd, "settings-prompts")
	cliPrompts := filepath.Join(cwd, "cli-prompts")
	writeSettingsFile(t, filepath.Join(settingsPrompts, "shared.md"), "settings prompt")
	writeSettingsFile(t, filepath.Join(cliPrompts, "shared.md"), "CLI prompt")
	writeSettingsFile(t, filepath.Join(cliPrompts, "cli-only.md"), "CLI-only prompt")
	writeSettingsFile(t, filepath.Join(cwd, ".pi", "settings.json"),
		`{"skills": ["`+settingsSkills+`"], "prompts": ["`+settingsPrompts+`"]}`)

	settings := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})
	options := resourceOptions{
		SkillPaths: []string{cliSkills}, PromptTemplatePaths: []string{cliPrompts},
	}
	loaded := options.skills(cwd, agentDir, settings, true)
	if len(loaded.Skills) != 2 {
		t.Fatalf("skills = %d, want the deduped shared skill and the CLI-only skill", len(loaded.Skills))
	}
	if got := loaded.Skills[0].Description; got != "from settings" {
		t.Errorf("skill collision kept %q, want settings", got)
	}
	if len(loaded.Skills) != 2 || loaded.Skills[1].Name != "cli-only" {
		t.Errorf("noncolliding CLI skill was not loaded: %v", loaded.Skills)
	}
	prompts := options.promptTemplates(cwd, agentDir, settings)
	if len(prompts.Templates) != 2 {
		t.Fatalf("prompt templates = %d, want shared and CLI-only", len(prompts.Templates))
	}
	if got := prompts.Templates[0].FilePath; got != filepath.Join(settingsPrompts, "shared.md") {
		t.Errorf("prompt collision kept %q, want settings path %q", got, filepath.Join(settingsPrompts, "shared.md"))
	}
	if got := prompts.Templates[1].Name; got != "cli-only" {
		t.Errorf("noncolliding CLI prompt = %q, want cli-only", got)
	}
}

// TestPromptTemplatePathsDeduplicateAcrossScopes follows pi v1.5.0
// (f9a26365) core/resource-loader.ts mergePaths, which canonicalizes and drops
// duplicate paths before any resources are loaded. Listing the same directory
// in both scope settings must not load the same template twice or report a
// false name collision.
func TestPromptTemplatePathsDeduplicateAcrossScopes(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	shared := filepath.Join(t.TempDir(), "shared-prompts")
	writeSettingsFile(t, filepath.Join(shared, "hello.md"), "Hello")
	writeSettingsFile(t, filepath.Join(cwd, ".pi", "settings.json"),
		`{"prompts": ["`+shared+`"]}`)
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"),
		`{"prompts": ["`+shared+`"]}`)

	settings := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})
	loaded := (resourceOptions{}).promptTemplates(cwd, agentDir, settings)
	if len(loaded.Templates) != 1 {
		t.Fatalf("templates = %d, want one", len(loaded.Templates))
	}
	if len(loaded.Diagnostics) != 0 {
		t.Errorf("duplicate path produced diagnostics: %#v", loaded.Diagnostics)
	}
}
