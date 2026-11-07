package coding

// The CLI's resource switches have to survive a reload. Upstream re-resolves its
// resource loader with the same options, so /reload is a re-read, not a reset:
// without this, --no-skills would quietly come back the moment a user reloaded,
// and an explicit --skill path would disappear.

// resourceOptions are the CLI-derived resource switches.
type resourceOptions struct {
	// SkillPaths are the --skill paths. They load even when NoSkills is set:
	// refusing discovery is not refusing what was named.
	SkillPaths []string
	// NoSkills suppresses skill discovery and the settings' skill paths.
	NoSkills bool
	// NoContextFiles suppresses AGENTS.md/CLAUDE.md discovery.
	NoContextFiles bool
	// PromptTemplatePaths are the --prompt-template paths, which load even when
	// NoPromptTemplates is set, for the same reason as skills.
	PromptTemplatePaths []string
	// NoPromptTemplates suppresses prompt-template discovery and the settings'
	// prompt-template paths.
	NoPromptTemplates bool
}

// scopedListPaths returns one array setting from the project scope and then the
// agent scope. The merged view cannot be used for a list: deep-merge replaces
// arrays, so a project value would hide the agent's, while upstream
// core/package-manager.ts resolve() adds project overrides only when trusted and
// user overrides always, both into one path map (A398).
func scopedListPaths(settings *SettingsManager, pick func(*Settings) []string) []string {
	if settings == nil {
		return nil
	}
	var paths []string
	if project := settings.GetProjectSettings(); project != nil {
		paths = append(paths, pick(project)...)
	}
	if global := settings.GetGlobalSettings(); global != nil {
		paths = append(paths, pick(global)...)
	}
	return paths
}

// contextFiles returns the project context files.
func (o resourceOptions) contextFiles(cwd string, agentDir string) []ContextFile {
	if o.NoContextFiles {
		return nil
	}
	return LoadProjectContextFiles(cwd, agentDir)
}

// skills returns the skills, from the explicit paths and (unless discovery is
// off) the settings'.
func (o resourceOptions) skills(cwd string, agentDir string, settings *SettingsManager, trusted bool) LoadSkillsResult {
	// CLI paths before the settings' (upstream mergePaths([...cliEnabledSkills,
	// ...enabledSkills], additionalSkillPaths)), so a --skill path wins a name
	// collision (A399).
	skillPaths := append([]string{}, o.SkillPaths...)
	if !o.NoSkills {
		skillPaths = append(skillPaths, scopedListPaths(settings, func(settings *Settings) []string {
			return settings.Skills
		})...)
	}
	return LoadSkills(LoadSkillsOptions{
		Cwd: cwd, AgentDir: agentDir, SkillPaths: skillPaths,
		IncludeDefaults: !o.NoSkills,
	}, trusted)
}

// promptTemplates returns the file-based prompt templates.
func (o resourceOptions) promptTemplates(cwd string, agentDir string, settings *SettingsManager) LoadPromptTemplatesResult {
	promptPaths := append([]string{}, o.PromptTemplatePaths...)
	if !o.NoPromptTemplates {
		promptPaths = append(promptPaths, scopedListPaths(settings, func(settings *Settings) []string {
			return settings.Prompts
		})...)
	}
	loaded := LoadPromptTemplatesWithDiagnostics(LoadPromptTemplatesOptions{
		Cwd: cwd, AgentDir: agentDir, PromptPaths: promptPaths,
		IncludeDefaults: !o.NoPromptTemplates,
	})
	// v1.2.0 resource-loader.ts dedupePrompts keeps the first discovered name.
	result := LoadPromptTemplatesResult{Diagnostics: loaded.Diagnostics}
	seen := map[string]PromptTemplate{}
	for _, template := range loaded.Templates {
		if winner, ok := seen[template.Name]; ok {
			result.Diagnostics = append(result.Diagnostics, ResourceDiagnostic{
				Type: "collision", Message: `name "/` + template.Name + `" collision`, Path: template.FilePath,
				Collision: &ResourceCollision{ResourceType: "prompt", Name: template.Name, WinnerPath: winner.FilePath, LoserPath: template.FilePath},
			})
		} else {
			seen[template.Name] = template
			result.Templates = append(result.Templates, template)
		}
	}
	return result
}
