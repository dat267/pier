package coding

import (
	"context"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Reload re-reads the settings and the ported resource files the session was
// built from, then rebuilds the system prompt (port of AgentSession.reload
// without the extension runner, the package manager, and their resource pass —
// D41/D140).
//
// The tool registry itself is unchanged: the built-in tools capture no
// settings-dependent state (the bash tool reads the shell settings per call).
// The active selection can still change when the session's initial tools came
// from defaultTools (db6cc71dc). Prompt templates and theme files are not
// loaded by the port at boot, so they are not
// re-read here either.
func (s *AgentSession) Reload() {
	// Previous resolved defaultTools, captured before the settings re-read:
	// reload activates only names the setting newly adds (db6cc71dc).
	previousDefaultTools := map[string]bool{}
	if s.usesDefaultTools && s.control.Settings != nil {
		for _, name := range resolvedDefaultToolSelection(s.control.Settings) {
			previousDefaultTools[name] = true
		}
	}
	if s.control.Settings != nil {
		s.control.Settings.Reload()
	}
	s.SyncQueueModesFromSettings()
	ai.ResetAPIProviders()
	s.reloadResources()
	if s.usesDefaultTools && s.control.Settings != nil {
		// Activate tools newly added to defaultTools. Removed ones stay active,
		// and tools disabled during the session stay disabled unless the setting
		// newly adds them; explicit --tools/--no-tools sessions never take this
		// path (upstream _buildRuntime + _refreshToolRegistry).
		names := append([]string{}, s.ActiveToolNames()...)
		for _, name := range resolvedDefaultToolSelection(s.control.Settings) {
			if !previousDefaultTools[name] {
				names = append(names, name)
			}
		}
		filtered := make([]string, 0, len(names))
		seen := map[string]bool{}
		for _, name := range names {
			if seen[name] || s.excludedToolNames[name] {
				continue
			}
			seen[name] = true
			filtered = append(filtered, name)
		}
		s.SetActiveToolsByName(filtered)
	} else {
		s.RebuildSystemPrompt(s.ActiveToolNames())
	}
	// defaultsync (D151): a reload is a session start too, so the settings
	// default reasserts itself over any model the session picked earlier.
	if !s.defaultSyncSuspended() {
		s.recordDefaultSync(s.SyncSessionModelToDefault(context.Background(), "reload"))
	}
}

// resolvedDefaultToolSelection is the effective defaultTools selection: the
// setting's resolved list, or the built-in defaults when unset (upstream
// `settingsManager.getDefaultTools() ?? DEFAULT_TOOL_NAMES`).
func resolvedDefaultToolSelection(settings *SettingsManager) []string {
	resolved := settings.GetDefaultTools()
	if resolved == nil {
		resolved = DefaultToolNames
	}
	return resolved
}

// excludedToolNameSet builds the reload exclusion lookup.
func excludedToolNameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// reloadResources re-reads the context files, skills, and system/append prompt
// files, and refreshes the system-prompt options (upstream
// ResourceLoader.reload's non-extension half).
func (s *AgentSession) reloadResources() {
	trusted := false
	if s.control.Settings != nil {
		trusted = s.control.Settings.IsProjectTrusted()
	}
	contextFiles := s.resources.contextFiles(s.Cwd, s.agentDir)
	skills := s.resources.skills(s.Cwd, s.agentDir, s.control.Settings, trusted)
	promptTemplates := s.resources.promptTemplates(s.Cwd, s.agentDir, s.control.Settings)
	overrides := LoadPromptOverrides(PromptFileSources{
		Cwd: s.Cwd, AgentDir: s.agentDir, ProjectTrusted: trusted,
		SystemPrompt: s.promptSources.SystemPrompt, AppendSystemPrompt: s.promptSources.AppendSystemPrompt,
	})

	s.promptOptionsMu.Lock()
	if s.SystemPromptOptions != nil {
		options := *s.SystemPromptOptions
		options.CustomPrompt = overrides.SystemPrompt
		options.AppendSystemPrompt = overrides.AppendSystemPrompt
		options.PromptSourcePaths = append([]string{}, overrides.SourcePaths...)
		options.Skills = skills.Skills
		options.ContextFiles = contextFiles
		s.SystemPromptOptions = &options
	}
	s.promptOptionsMu.Unlock()
	s.skillDiagnostics = skills.Diagnostics
	s.control.PromptTemplates = promptTemplates
}

// ActiveToolNames returns the active tools' names in order (upstream
// AgentSession.getActiveToolNames).
func (s *AgentSession) ActiveToolNames() []string {
	tools := s.Agent.State().Tools
	return agentToolNames(tools)
}

func agentToolNames(tools []agent.AgentTool) []string {
	names := make([]string, 0, len(tools))
	for i := range tools {
		names = append(names, tools[i].Name)
	}
	return names
}
