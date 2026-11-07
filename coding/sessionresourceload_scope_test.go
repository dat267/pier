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

// TestResourcePathsPutCLIPathsFirst pins the collision tie-break. Reference:
// pi v1.5.0 (f9a26365) core/resource-loader.ts builds the list as
// mergePaths([...cliEnabledSkills, ...enabledSkills], additionalSkillPaths), so
// a --skill path precedes the settings' paths and wins a name collision
// (dedupePrompts/dedupe keep the first occurrence).
func TestResourcePathsPutCLIPathsFirst(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	settingsSkills := filepath.Join(cwd, "settings-skills")
	cliSkills := filepath.Join(cwd, "cli-skills")
	writeSkill(t, settingsSkills, "shared", "from settings")
	writeSkill(t, cliSkills, "shared", "from the flag")
	writeSettingsFile(t, filepath.Join(cwd, ".pi", "settings.json"),
		`{"skills": ["`+settingsSkills+`"]}`)

	settings := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})
	options := resourceOptions{SkillPaths: []string{cliSkills}}
	loaded := options.skills(cwd, agentDir, settings, true)
	if len(loaded.Skills) != 1 {
		t.Fatalf("skills = %d, want one deduped skill", len(loaded.Skills))
	}
	if got := loaded.Skills[0].Description; got != "from the flag" {
		t.Errorf("collision kept %q, want the --skill path to win", got)
	}
}
