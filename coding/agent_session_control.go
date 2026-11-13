package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// Port of the state/control surface of core/agent-session.ts: read-only state
// access, tool management, queue modes, model and thinking-level management,
// auto toggles, bash state, and context usage.
//
// D41: upstream's session owns an ExtensionRunner and a resource loader; both
// are extension mechanics and out of scope, so the corresponding accessors
// (extensionRunner, hasExtensionHandlers, bindExtensions, promptTemplates via
// the resource loader) are omitted or take their data from the session config.

// QueueMode controls how queued messages drain.
type QueueMode = string

const (
	QueueModeAll        QueueMode = "all"
	QueueModeOneAtATime QueueMode = "one-at-a-time"
)

// ToolInfo describes one configured tool.
type ToolInfo struct {
	Name             string
	Description      string
	Parameters       json.RawMessage
	PromptGuidelines []string
	SourceInfo       *SourceInfo
}

// ModelMutationOptions control model/thinking mutations.
type ModelMutationOptions struct {
	// Persist writes the change to global settings.
	Persist bool
}

// ModelCycleResult is the outcome of cycling a model.
type ModelCycleResult struct {
	Model         *ai.Model
	ThinkingLevel ai.ThinkingLevel
	IsScoped      bool
}

// AgentSessionControl holds the optional runtime collaborators and mutation
// state ported here.
type AgentSessionControl struct {
	// ModelRuntime resolves availability and auth for model switches.
	ModelRuntime *ModelRuntime
	// Settings supplies defaults and persists changes.
	Settings *SettingsManager
	// Tools is the registry of selectable tools keyed by name.
	Tools map[string]AgentToolDefinition
	// PromptTemplates are the file-based prompt templates (resource loader).
	PromptTemplates []PromptTemplate

	stateMu              sync.Mutex
	scopedModels         []ScopedModel
	scopedModelsRevision uint64
	autoCompaction       bool
	autoRetry            bool
	branchSummaryCancel  context.CancelFunc

	// defaultSync is the last defaultsync outcome (D151), and
	// defaultSyncSuspended records an explicit model override that the sync
	// must not overrule.
	defaultSync          DefaultSyncResult
	defaultSyncSuspended bool
}

// AgentToolDefinition is a registered tool plus its source metadata.
type AgentToolDefinition struct {
	Tool       agent.AgentTool
	SourceInfo *SourceInfo
	// PromptSnippet is the tool's one-line prompt entry (upstream
	// `definition.promptSnippet`). Tools without one are not listed in the
	// system prompt's tools section.
	PromptSnippet string
	// PromptGuidelines are the tool's prompt bullets (upstream reads these
	// from the tool definition).
	PromptGuidelines []string
}

// State returns the underlying agent state.
func (s *AgentSession) State() agent.AgentState { return s.Agent.State() }

// Model returns the current model.
func (s *AgentSession) Model() *ai.Model { return s.Agent.State().Model }

// ModelRuntime returns the model runtime backing the session (nil when the
// session was created without one).
func (s *AgentSession) ModelRuntime() *ModelRuntime {
	return s.control.ModelRuntime
}

// GetAvailableModels returns the runtime's available-model snapshot (the
// settings selector's per-model list; upstream reads the same snapshot).
func (s *AgentSession) GetAvailableModels() []*ai.Model {
	runtime := s.ModelRuntime()
	if runtime == nil {
		return nil
	}
	return runtime.GetAvailableSnapshot()
}

// GetRetryAttempt is the transcript's accessor name for RetryAttempt.
func (s *AgentSession) GetRetryAttempt() int { return s.RetryAttempt() }

// GetModelPriceSource returns the runtime as the cache-price source.
func (s *AgentSession) GetModelPriceSource() ModelPriceSource {
	runtime := s.ModelRuntime()
	if runtime == nil {
		return nil
	}
	return runtime
}

// HasModel reports whether a real model is selected. The agent keeps a
// placeholder model when none was configured, which upstream represents as an
// undefined model.
func (s *AgentSession) HasModel() bool {
	model := s.Model()
	return model != nil && model.ID != "" && model.ID != "unknown"
}

// ThinkingLevel returns the current thinking level.
func (s *AgentSession) ThinkingLevel() ai.ThinkingLevel { return s.Agent.State().ThinkingLevel }

// IsStreaming reports whether an agent run is active (upstream's
// _isAgentRunActive, which spans prompt, compaction continuation, and retry).
func (s *AgentSession) IsStreaming() bool {
	if s.Agent.State().IsStreaming {
		return true
	}
	// promptState is created lazily under s.mu, so it must be read under the
	// same lock; a bare field read races the first Prompt.
	s.mu.Lock()
	promptState := s.promptState
	s.mu.Unlock()
	if promptState == nil {
		return false
	}
	promptState.mu.Lock()
	defer promptState.mu.Unlock()
	return promptState.runActive
}

// compactionInFlight reports whether a manual or automatic compaction is
// running. Upstream derives isCompacting from the abort controllers
// (agent-session.ts get isCompacting); the port keeps the single
// compactionCancel and derives from it instead of a parallel flag.
func (s *AgentSession) compactionInFlight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactionCancel != nil
}

// branchSummaryInFlight reports whether a branch summarization is running.
func (s *AgentSession) branchSummaryInFlight() bool {
	s.control.stateMu.Lock()
	defer s.control.stateMu.Unlock()
	return s.control.branchSummaryCancel != nil
}

// IsCompacting reports whether compaction or branch summarization is running.
func (s *AgentSession) IsCompacting() bool {
	return s.compactionInFlight() || s.branchSummaryInFlight()
}

// IsIdle reports whether no agent run or compaction is active.
func (s *AgentSession) IsIdle() bool { return !s.IsStreaming() && !s.IsCompacting() }

// SystemPrompt returns the effective system prompt.
func (s *AgentSession) SystemPrompt() string {
	if options, ok := s.resolvedSystemPromptOptions(); ok {
		prompt, err := BuildSystemPrompt(options)
		if err == nil {
			return prompt
		}
	}
	return s.Agent.State().SystemPrompt
}

// RetryAttempt returns the current retry attempt (0 when not retrying).
func (s *AgentSession) RetryAttempt() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retryAttempt
}

// Messages returns the transcript including custom message types.
func (s *AgentSession) Messages() []ai.Message { return s.Agent.State().Messages }

// SteeringMode returns the current steering drain mode.
func (s *AgentSession) SteeringMode() QueueMode { return s.Agent.SteeringMode() }

// FollowUpMode returns the current follow-up drain mode.
func (s *AgentSession) FollowUpMode() QueueMode { return s.Agent.FollowUpMode() }

// SetSteeringMode sets the steering drain mode.
func (s *AgentSession) SetSteeringMode(mode QueueMode) { s.Agent.SetSteeringMode(mode) }

// SetFollowUpMode sets the follow-up drain mode.
func (s *AgentSession) SetFollowUpMode(mode QueueMode) { s.Agent.SetFollowUpMode(mode) }

// SetTransport changes the request transport for subsequent agent turns.
func (s *AgentSession) SetTransport(transport ai.Transport) { s.Agent.SetTransport(transport) }

// SyncQueueModesFromSettings applies the settings-backed queue modes.
func (s *AgentSession) SyncQueueModesFromSettings() {
	if s.control.Settings == nil {
		return
	}
	s.Agent.SetSteeringMode(s.control.Settings.GetSteeringMode())
	s.Agent.SetFollowUpMode(s.control.Settings.GetFollowUpMode())
}

// ClearQueue drains both queues and reports what was removed.
func (s *AgentSession) ClearQueue() (steering []string, followUp []string) {
	s.mu.Lock()
	steering = append([]string{}, s.steeringMessages...)
	followUp = append([]string{}, s.followUpMessages...)
	s.Agent.ClearAllQueues()
	s.steeringMessages = nil
	s.followUpMessages = nil
	s.mu.Unlock()
	s.emitQueueUpdate()
	return steering, followUp
}

// PendingMessageCount is the number of queued steering and follow-up messages.
func (s *AgentSession) PendingMessageCount() int {
	return len(s.GetSteeringMessages()) + len(s.GetFollowUpMessages())
}

// GetSteeringMessages lists the queued steering message texts.
func (s *AgentSession) GetSteeringMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.steeringMessages...)
}

// GetFollowUpMessages lists the queued follow-up message texts.
func (s *AgentSession) GetFollowUpMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.followUpMessages...)
}

// SessionFile returns the session file path ("" when disabled).
func (s *AgentSession) SessionFile() string { return s.Sessions.GetSessionFile() }

// SessionID returns the session id.
func (s *AgentSession) SessionID() string { return s.Sessions.GetSessionID() }

// SessionName returns the current display name.
func (s *AgentSession) SessionName() string { return s.Sessions.GetSessionName() }

// SetSessionName records a display name for the session.
func (s *AgentSession) SetSessionName(name string) {
	s.Sessions.AppendSessionInfo(name)
	var currentName *string
	if name := s.Sessions.GetSessionName(); name != "" {
		currentName = &name
	}
	s.emit(&SessionEvent{Type: SessionInfoChanged, Name: currentName})
}

// ScopedModels returns the model cycle scope.
func (s *AgentSession) ScopedModels() []ScopedModel {
	models, _ := s.ScopedModelsWithRevision()
	return models
}

// ScopedModelsWithRevision returns a defensive scope copy and its revision from
// the same locked snapshot.
func (s *AgentSession) ScopedModelsWithRevision() ([]ScopedModel, uint64) {
	s.control.stateMu.Lock()
	models := append([]ScopedModel{}, s.control.scopedModels...)
	revision := s.control.scopedModelsRevision
	s.control.stateMu.Unlock()
	if len(models) == 0 {
		return nil, revision
	}
	return models, revision
}

// ScopedModelsRevision changes whenever SetScopedModels replaces the scope.
func (s *AgentSession) ScopedModelsRevision() uint64 {
	s.control.stateMu.Lock()
	revision := s.control.scopedModelsRevision
	s.control.stateMu.Unlock()
	return revision
}

// SetScopedModels replaces the model cycle scope.
func (s *AgentSession) SetScopedModels(models []ScopedModel) {
	s.control.stateMu.Lock()
	s.control.scopedModels = append([]ScopedModel{}, models...)
	s.control.scopedModelsRevision++
	s.control.stateMu.Unlock()
}

// PromptTemplates returns the file-based prompt templates.
func (s *AgentSession) PromptTemplates() []PromptTemplate {
	s.promptOptionsMu.Lock()
	defer s.promptOptionsMu.Unlock()
	if len(s.control.PromptTemplates) == 0 {
		return nil
	}
	return append([]PromptTemplate{}, s.control.PromptTemplates...)
}

// Skills returns the loaded skills (the system-prompt options).
func (s *AgentSession) Skills() []Skill {
	options, ok := s.systemPromptOptionsSnapshot()
	if !ok {
		return nil
	}
	return append([]Skill{}, options.Skills...)
}

// ContextFiles returns the loaded context files (the system-prompt options).
// PromptSourcePaths returns the loaded system/append prompt files, base prompt
// first (upstream getSystemPromptSource / getAppendSystemPromptSources).
func (s *AgentSession) PromptSourcePaths() []string {
	options, ok := s.systemPromptOptionsSnapshot()
	if !ok {
		return nil
	}
	return append([]string{}, options.PromptSourcePaths...)
}

func (s *AgentSession) ContextFiles() []ContextFile {
	options, ok := s.systemPromptOptionsSnapshot()
	if !ok {
		return nil
	}
	return append([]ContextFile{}, options.ContextFiles...)
}

// PromptDiagnostics returns prompt loader warnings and name collisions.
func (s *AgentSession) PromptDiagnostics() []ResourceDiagnostic {
	s.promptOptionsMu.Lock()
	defer s.promptOptionsMu.Unlock()
	return append([]ResourceDiagnostic{}, s.promptDiagnostics...)
}

// SkillDiagnostics returns the skill loader's warnings/collisions.
func (s *AgentSession) SkillDiagnostics() []ResourceDiagnostic {
	s.promptOptionsMu.Lock()
	defer s.promptOptionsMu.Unlock()
	return append([]ResourceDiagnostic{}, s.skillDiagnostics...)
}

// AutoCompactionEnabled reports the auto-compaction toggle.
func (s *AgentSession) AutoCompactionEnabled() bool {
	s.control.stateMu.Lock()
	defer s.control.stateMu.Unlock()
	return s.control.autoCompaction
}

// SetAutoCompactionEnabled toggles auto-compaction.
func (s *AgentSession) SetAutoCompactionEnabled(enabled bool) {
	s.control.stateMu.Lock()
	s.control.autoCompaction = enabled
	s.control.stateMu.Unlock()
}

// AutoRetryEnabled reports the auto-retry toggle.
func (s *AgentSession) AutoRetryEnabled() bool {
	s.control.stateMu.Lock()
	defer s.control.stateMu.Unlock()
	return s.control.autoRetry
}

// SetAutoRetryEnabled toggles auto-retry.
func (s *AgentSession) SetAutoRetryEnabled(enabled bool) {
	s.control.stateMu.Lock()
	s.control.autoRetry = enabled
	s.control.stateMu.Unlock()
}

// GetActiveToolNames lists the tools currently set on the agent.
func (s *AgentSession) GetActiveToolNames() []string {
	tools := s.Agent.State().Tools
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// GetAllTools lists every configured tool with its metadata.
func (s *AgentSession) GetAllTools() []ToolInfo {
	s.control.stateMu.Lock()
	defer s.control.stateMu.Unlock()
	if len(s.control.Tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.control.Tools))
	for name := range s.control.Tools {
		names = append(names, name)
	}
	sortStrings(names)
	out := make([]ToolInfo, 0, len(names))
	for _, name := range names {
		entry := s.control.Tools[name]
		out = append(out, ToolInfo{
			Name:             entry.Tool.Name,
			Description:      entry.Tool.Description,
			Parameters:       entry.Tool.Parameters,
			PromptGuidelines: entry.PromptGuidelines,
			SourceInfo:       entry.SourceInfo,
		})
	}
	return out
}

// GetToolDefinition returns a registered tool by name.
func (s *AgentSession) GetToolDefinition(name string) *agent.AgentTool {
	s.control.stateMu.Lock()
	defer s.control.stateMu.Unlock()
	entry, ok := s.control.Tools[name]
	if !ok {
		return nil
	}
	return &entry.Tool
}

// AttachExtraTools reconciles caller-supplied tools (the MCP manager's current
// direct-tool snapshot) with the live session. MCP tools replace same-named
// definitions and tools absent from the snapshot are removed. New tools become
// active unless the session's allowlist or exclusions reject them; the returned
// list contains only names newly added to the registry.
func (s *AgentSession) AttachExtraTools(tools []agent.AgentTool) []string {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	incomingMcp := make(map[string]agent.AgentTool, len(tools))
	for _, tool := range tools {
		if tool.Name != "" && strings.HasPrefix(tool.Name, "mcp__") {
			incomingMcp[tool.Name] = tool
		}
	}

	s.control.stateMu.Lock()
	changed := false
	for name := range s.control.Tools {
		if !strings.HasPrefix(name, "mcp__") {
			continue
		}
		if _, ok := incomingMcp[name]; !ok {
			delete(s.control.Tools, name)
			changed = true
		}
	}
	added := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			continue
		}
		if _, ok := s.control.Tools[tool.Name]; ok {
			if strings.HasPrefix(tool.Name, "mcp__") {
				s.control.Tools[tool.Name] = AgentToolDefinition{Tool: tool}
				changed = true
			}
			continue
		}
		s.control.Tools[tool.Name] = AgentToolDefinition{Tool: tool}
		added = append(added, tool.Name)
		changed = true
	}
	s.control.stateMu.Unlock()
	if !changed {
		return nil
	}
	names := append([]string{}, s.ActiveToolNames()...)
	names = append(names, added...)
	filtered := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] ||
			(s.allowedToolNames != nil && !matchesAnyToolPattern(s.allowedToolNames, name)) ||
			matchesAnyToolPattern(s.excludedToolNames, name) {
			continue
		}
		seen[name] = true
		filtered = append(filtered, name)
	}
	s.setActiveToolsByNameLocked(filtered)
	if len(added) == 0 {
		return nil
	}
	return added
}

// registerGoalTools adds the goal tools to the session's registry with their
// prompt guidelines. They are registered once; exposure is gated by the goal
// controller's SyncTools.
func (s *AgentSession) registerGoalTools() {
	if s.Goal == nil || s.control == nil {
		return
	}
	for _, tool := range s.Goal.Tools() {
		if _, ok := s.control.Tools[tool.Name]; !ok {
			s.control.Tools[tool.Name] = AgentToolDefinition{
				Tool:             tool,
				PromptSnippet:    goalToolSnippets[tool.Name],
				PromptGuidelines: goalToolGuidelines[tool.Name],
			}
		}
	}
}

// SetActiveToolsByName enables named registry tools allowed by this session's
// allowlist and exclusions, then rebuilds the system prompt.
func (s *AgentSession) SetActiveToolsByName(names []string) {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	s.setActiveToolsByNameLocked(names)
}

// syncGoalTools exposes the goal tools the goal feature wants, reading and
// setting the active set in one step under toolsMu.
func (s *AgentSession) syncGoalTools() {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	current := s.ActiveToolNames()
	next := goal.DesiredToolNames(current)
	if !sameToolSet(next, current) {
		s.setActiveToolsByNameLocked(next)
	}
}

// setActiveToolsByNameLocked is SetActiveToolsByName for a caller holding
// toolsMu.
func (s *AgentSession) setActiveToolsByNameLocked(names []string) {
	// The active tools are a set: tools that stay active keep their current
	// order and newly active ones append, so reordering the same names does not
	// change the declared tool order or the prompt (pi v1.5.0 b9e3eef6c).
	requested := make(map[string]struct{}, len(names))
	for _, name := range names {
		requested[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for _, name := range s.GetActiveToolNames() {
		if _, ok := requested[name]; ok {
			ordered = append(ordered, name)
			delete(requested, name)
		}
	}
	for _, name := range names {
		if _, ok := requested[name]; ok {
			ordered = append(ordered, name)
			delete(requested, name)
		}
	}
	s.control.stateMu.Lock()
	tools := make([]agent.AgentTool, 0, len(ordered))
	validNames := make([]string, 0, len(ordered))
	for _, name := range ordered {
		if (s.allowedToolNames != nil && !matchesAnyToolPattern(s.allowedToolNames, name)) ||
			matchesAnyToolPattern(s.excludedToolNames, name) {
			continue
		}
		entry, ok := s.control.Tools[name]
		if !ok {
			continue
		}
		tools = append(tools, entry.Tool)
		validNames = append(validNames, name)
	}
	s.control.stateMu.Unlock()
	s.Agent.SetTools(tools)
	s.RebuildSystemPrompt(validNames)
}

// RebuildSystemPrompt refreshes the session prompt for a tool set.
func (s *AgentSession) RebuildSystemPrompt(toolNames []string) {
	s.promptOptionsMu.Lock()
	defer s.promptOptionsMu.Unlock()
	if s.SystemPromptOptions == nil {
		return
	}
	options := *s.SystemPromptOptions
	options.SelectedTools = toolNames
	s.SystemPromptOptions = &options
}

// systemPromptOptionsSnapshot returns a copy of the prompt options.
func (s *AgentSession) systemPromptOptionsSnapshot() (BuildSystemPromptOptions, bool) {
	s.promptOptionsMu.Lock()
	defer s.promptOptionsMu.Unlock()
	if s.SystemPromptOptions == nil {
		return BuildSystemPromptOptions{}, false
	}
	return *s.SystemPromptOptions, true
}

// resolvedSystemPromptOptions snapshots the prompt options and fills the
// registry-derived inputs (active tool names, snippets, guidelines).
func (s *AgentSession) resolvedSystemPromptOptions() (BuildSystemPromptOptions, bool) {
	options, ok := s.systemPromptOptionsSnapshot()
	if !ok {
		return options, false
	}
	s.control.stateMu.Lock()
	options.SelectedTools = selectedRegistryTools(options.SelectedTools, s.control.Tools)
	s.control.stateMu.Unlock()
	options.ToolSnippets, options.ToolGuidelines = s.toolPromptContributions()
	return options, true
}

// preparePromptAndToolLoadout resolves the active tool registry set and returns
// a system message carrying the prompt-section diff, or nil when nothing
// changed (port of _preparePromptAndToolLoadout). It updates the agent's tool
// set as a side effect. The message is installed in the transcript by the
// caller; the agent loop declares tool changes itself.
func (s *AgentSession) preparePromptAndToolLoadout() *ai.SystemMessage {
	options, ok := s.resolvedSystemPromptOptions()
	if !ok {
		return nil
	}

	s.control.stateMu.Lock()
	tools := make([]agent.AgentTool, 0, len(options.SelectedTools))
	for _, name := range options.SelectedTools {
		tools = append(tools, s.control.Tools[name].Tool)
	}
	s.control.stateMu.Unlock()
	s.Agent.SetTools(tools)

	previous := map[string]string{}
	if current := ai.GetCurrentSystemMessage(s.Agent.State().Messages); current != nil {
		for name, value := range current.Sections {
			if value != nil {
				previous[name] = *value
			}
		}
	}
	built, order, err := buildSystemPromptSections(options)
	if err != nil {
		return nil
	}
	patch := DiffSystemPromptSections(previous, built)
	if len(patch) == 0 {
		return nil
	}
	message := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: ""}, Timestamp: time.Now().UnixMilli()}
	for _, name := range order {
		if value, ok := patch[name]; ok {
			message.SetSection(name, value)
		}
	}
	var removed []string
	for name := range patch {
		if _, ok := built[name]; !ok {
			removed = append(removed, name)
		}
	}
	sortNames(removed)
	for _, name := range removed {
		message.SetSection(name, nil)
	}
	return message
}

// SetModel switches the session model (auth-checked) and applies the thinking
// level for the new model.
func (s *AgentSession) SetModel(ctx context.Context, model *ai.Model, options ModelMutationOptions) error {
	if s.control.ModelRuntime != nil && !s.control.ModelRuntime.HasConfiguredAuth(model.Provider) {
		return fmt.Errorf("No API key for %s/%s", model.Provider, model.ID)
	}
	previous := s.Model()
	s.Agent.SetModel(model)
	s.Sessions.AppendModelChange(model.Provider, model.ID)
	if options.Persist && s.control.Settings != nil {
		s.control.Settings.SetDefaultModelAndProvider(model.Provider, model.ID)
		s.addPersistedDefaultToNonEmptyScope(model)
	}
	// Applying the thinking level for the new model must not persist it:
	// upstream setModel calls setThinkingLevel with no options, so model
	// persistence never rewrites the global thinking default (agent-session.ts:2131).
	s.SetThinkingLevel(s.getThinkingLevelForModelSwitch(model, ""))
	s.emitModelSelect(model, previous, "set")
	return nil
}

// nextCycleIndex is the model to cycle to, and false when there is nothing to
// switch to (pi v1.5.2 agent-session.ts getNextCycleIndex). When the current
// model is not in the list (currentIndex -1), forward picks the first entry and
// backward the last, so a one-entry list still selects its only model.
func nextCycleIndex(currentIndex, length int, direction string) (int, bool) {
	if length == 0 {
		return 0, false
	}
	if currentIndex == -1 {
		if direction == "forward" {
			return 0, true
		}
		return length - 1, true
	}
	if length == 1 {
		return 0, false
	}
	if direction == "forward" {
		return (currentIndex + 1) % length, true
	}
	return (currentIndex - 1 + length) % length, true
}

// CycleModel advances to the next (or previous) model in scope or availability.
func (s *AgentSession) CycleModel(ctx context.Context, direction string, options ModelMutationOptions) (*ModelCycleResult, error) {
	if len(s.ScopedModels()) > 0 {
		return s.cycleScopedModel(ctx, direction, options)
	}
	return s.cycleAvailableModel(ctx, direction, options)
}

func (s *AgentSession) cycleScopedModel(ctx context.Context, direction string, options ModelMutationOptions) (*ModelCycleResult, error) {
	available := s.availableModelKeys()
	scoped := make([]ScopedModel, 0, len(s.ScopedModels()))
	for _, candidate := range s.ScopedModels() {
		if available[candidate.Model.Provider+"\x00"+candidate.Model.ID] {
			scoped = append(scoped, candidate)
		}
	}
	current := s.Model()
	index := -1
	for position, candidate := range scoped {
		if ai.ModelsAreEqual(candidate.Model, current) {
			index = position
			break
		}
	}
	nextIndex, ok := nextCycleIndex(index, len(scoped), direction)
	if !ok {
		return nil, nil
	}
	next := scoped[nextIndex]
	return s.applyModelCycle(ctx, next.Model, next.ThinkingLevel, options, true)
}

func (s *AgentSession) cycleAvailableModel(ctx context.Context, direction string, options ModelMutationOptions) (*ModelCycleResult, error) {
	var models []*ai.Model
	if s.control.ModelRuntime != nil {
		models = s.control.ModelRuntime.GetAvailableSnapshot()
	}
	current := s.Model()
	index := -1
	for position, candidate := range models {
		if ai.ModelsAreEqual(candidate, current) {
			index = position
			break
		}
	}
	nextIndex, ok := nextCycleIndex(index, len(models), direction)
	if !ok {
		return nil, nil
	}
	next := models[nextIndex]
	return s.applyModelCycle(ctx, next, "", options, false)
}

func (s *AgentSession) applyModelCycle(ctx context.Context, model *ai.Model, explicitLevel ai.ThinkingLevel, options ModelMutationOptions, isScoped bool) (*ModelCycleResult, error) {
	if s.control.ModelRuntime != nil && !s.control.ModelRuntime.HasConfiguredAuth(model.Provider) {
		return nil, fmt.Errorf("No API key for %s/%s", model.Provider, model.ID)
	}
	previous := s.Model()
	s.Agent.SetModel(model)
	s.Sessions.AppendModelChange(model.Provider, model.ID)
	if options.Persist && s.control.Settings != nil {
		s.control.Settings.SetDefaultModelAndProvider(model.Provider, model.ID)
		s.addPersistedDefaultToNonEmptyScope(model)
	}
	// As in SetModel, a model change never persists the thinking level:
	// upstream cycleScopedModel/cycleAvailableModel call setThinkingLevel with no
	// options (agent-session.ts:2204,2236).
	s.SetThinkingLevel(s.getThinkingLevelForModelSwitch(model, explicitLevel))
	s.emitModelSelect(model, previous, "cycle")
	return &ModelCycleResult{Model: model, ThinkingLevel: s.ThinkingLevel(), IsScoped: isScoped}, nil
}

func (s *AgentSession) availableModelKeys() map[string]bool {
	keys := map[string]bool{}
	if s.control.ModelRuntime != nil {
		for _, model := range s.control.ModelRuntime.GetAvailableSnapshot() {
			keys[model.Provider+"\x00"+model.ID] = true
		}
	}
	return keys
}

// addPersistedDefaultToNonEmptyScope keeps a persisted default inside a
// non-empty model scope.
func (s *AgentSession) addPersistedDefaultToNonEmptyScope(model *ai.Model) {
	if s.control.Settings == nil {
		return
	}
	s.control.stateMu.Lock()
	scoped := append([]ScopedModel{}, s.control.scopedModels...)
	s.control.stateMu.Unlock()
	if len(scoped) == 0 {
		return
	}
	for _, candidate := range scoped {
		if ai.ModelsAreEqual(candidate.Model, model) {
			return
		}
	}
	scoped = append(scoped, ScopedModel{Model: model})
	s.SetScopedModels(scoped)

	enabledModels := s.control.Settings.GetEnabledModels()
	if len(enabledModels) == 0 {
		return
	}
	reference := fmt.Sprintf("%s/%s", model.Provider, model.ID)
	for _, pattern := range enabledModels {
		if strings.EqualFold(pattern, reference) {
			return
		}
	}
	s.control.Settings.SetEnabledModels(append(enabledModels, reference))
}

// recordSelection records the live model and thinking level on the current
// branch, so a resume restores them (pi v1.5.2 agent-session.ts
// _recordSelection). Tree navigation can leave the latest model or level change
// on another branch.
func (s *AgentSession) recordSelection() {
	branch := s.Sessions.GetBranch("")
	s.recordModelSelection(branch)
	s.recordThinkingLevel(branch)
}

// recordModelSelection records the model the branch implies when it differs. A
// response cannot record a virtual selection because it names a physical model,
// so a physical record is replaced only when a virtual model runs.
func (s *AgentSession) recordModelSelection(branch []SessionEntry) {
	model := s.Model()
	if model == nil {
		return
	}
	getModel := func(provider, modelID string) *ai.Model {
		if s.control.ModelRuntime == nil {
			return nil
		}
		return s.control.ModelRuntime.GetModel(provider, modelID)
	}
	recorded := GetBranchSelection(branch, getModel)
	if recorded == nil || (recorded.Provider == model.Provider && recorded.ModelID == model.ID) {
		return
	}
	recordedModel := getModel(recorded.Provider, recorded.ModelID)
	if !IsVirtualModel(model) && (recordedModel == nil || !IsVirtualModel(recordedModel)) {
		return
	}
	s.Sessions.AppendModelChange(model.Provider, model.ID)
}

// recordThinkingLevel records the live level when the branch's latest one
// differs, comparing after clamping to the current model so a clamped level is
// not recorded over the requested one (#11451).
func (s *AgentSession) recordThinkingLevel(branch []SessionEntry) {
	var recorded *SessionEntry
	for index := len(branch) - 1; index >= 0; index-- {
		if branch[index].Type == "thinking_level_change" {
			recorded = &branch[index]
			break
		}
	}
	if recorded == nil {
		return
	}
	recordedLevel := ai.ThinkingLevel(recorded.ThinkingLevel)
	effective := recordedLevel
	if available := s.GetAvailableThinkingLevels(); !slices.Contains(available, recordedLevel) {
		effective = s.clampThinkingLevel(recordedLevel)
	}
	if effective == s.ThinkingLevel() {
		return
	}
	s.Sessions.AppendThinkingLevelChange(s.ThinkingLevel())
}

// transcript entry when it changes, and optionally persists the requested level.
func (s *AgentSession) SetThinkingLevel(level ai.ThinkingLevel, optionList ...ModelMutationOptions) {
	options := ModelMutationOptions{}
	if len(optionList) > 0 {
		options = optionList[0]
	}
	available := s.GetAvailableThinkingLevels()
	effective := level
	found := slices.Contains(available, level)
	if !found {
		effective = s.clampThinkingLevel(level)
	}

	previous := s.ThinkingLevel()
	changed := effective != previous
	s.Agent.SetThinkingLevel(effective)

	if options.Persist && s.control.Settings != nil {
		s.control.Settings.SetDefaultThinkingLevel(level)
	}
	if changed {
		s.Sessions.AppendThinkingLevelChange(effective)
		s.emit(&SessionEvent{Type: SessionThinkingLevelChanged, Level: effective})
	}
}

// CycleThinkingLevel advances to the next available level.
func (s *AgentSession) CycleThinkingLevel(options ModelMutationOptions) (ai.ThinkingLevel, bool) {
	if !s.SupportsThinking() {
		return "", false
	}
	levels := s.GetAvailableThinkingLevels()
	if len(levels) == 0 {
		return "", false
	}
	current := s.ThinkingLevel()
	index := 0
	for position, level := range levels {
		if level == current {
			index = position
			break
		}
	}
	next := levels[(index+1)%len(levels)]
	s.SetThinkingLevel(next, options)
	return next, true
}

// GetAvailableThinkingLevels lists the levels the current model supports.
func (s *AgentSession) GetAvailableThinkingLevels() []ai.ThinkingLevel {
	model := s.Model()
	if model == nil {
		return append([]ai.ThinkingLevel{}, ThinkingLevelOptions...)
	}
	supported := ai.GetSupportedThinkingLevels(model)
	out := make([]ai.ThinkingLevel, 0, len(supported))
	out = append(out, supported...)
	return out
}

// SupportsThinking reports whether the current model supports reasoning.
func (s *AgentSession) SupportsThinking() bool {
	model := s.Model()
	return model != nil && model.Reasoning
}

// getThinkingLevelForModelSwitch resolves the level to apply on a model change:
// an explicit level wins, then the per-model default, then the global default,
// then the current level, then the built-in default.
func (s *AgentSession) getThinkingLevelForModelSwitch(target *ai.Model, explicit ai.ThinkingLevel) ai.ThinkingLevel {
	if explicit != "" {
		return explicit
	}
	if target != nil && s.control.Settings != nil {
		if perModel := s.control.Settings.GetModelThinkingLevel(target.Provider, target.ID); perModel != nil {
			return *perModel
		}
	}
	if s.control.Settings != nil {
		if global := s.control.Settings.GetDefaultThinkingLevel(); global != nil {
			return *global
		}
	}
	if current := s.ThinkingLevel(); current != "" {
		return current
	}
	return DefaultThinkingLevel
}

func (s *AgentSession) clampThinkingLevel(level ai.ThinkingLevel) ai.ThinkingLevel {
	model := s.Model()
	if model == nil {
		return ai.ThinkOff
	}
	return ai.ClampThinkingLevel(model, level)
}

func (s *AgentSession) emitModelSelect(model, previous *ai.Model, source string) {
	// Upstream emits a model_select event; the Go session carries it on the
	// model cycle result and the message stream, so only the transcript entry
	// and thinking-level event are emitted here.
}

// GetLastAssistantText returns the last assistant message's trimmed text, or ""
// when there is none (port of getLastAssistantText).
func (s *AgentSession) GetLastAssistantText() string {
	messages := s.Messages()
	for _, message := range slices.Backward(messages) {
		assistant, ok := message.(*ai.AssistantMessage)
		if !ok {
			continue
		}
		// Skip aborted messages with no content.
		if assistant.StopReason == ai.StopAborted && len(assistant.Content) == 0 {
			continue
		}
		var builder strings.Builder
		for _, block := range assistant.Content {
			if text, ok := block.(ai.TextContent); ok {
				builder.WriteString(text.Text)
			}
		}
		if trimmed := ai.JSTrim(builder.String()); trimmed != "" {
			return trimmed
		}
		return ""
	}
	return ""
}

// UserMessageFork is one user message available for forking.
type UserMessageFork struct {
	EntryID string
	Text    string
}

// GetUserMessagesForForking lists the user messages with their entry ids.
func (s *AgentSession) GetUserMessagesForForking() []UserMessageFork {
	var out []UserMessageFork
	for _, entry := range s.Sessions.GetEntries() {
		if entry.Type != "message" {
			continue
		}
		message, err := ai.UnmarshalMessage(entry.Message)
		if err != nil {
			continue
		}
		user, ok := message.(*ai.UserMessage)
		if !ok {
			continue
		}
		text := contentTextJoinedNoSep(user.Content)
		if text != "" {
			out = append(out, UserMessageFork{EntryID: entry.ID, Text: text})
		}
	}
	return out
}

// ContextUsageReport is the context-window usage; Tokens and Percent are nil
// when the count is unknown (upstream's null).
type ContextUsageReport struct {
	Tokens        *int64
	ContextWindow int64
	Percent       *float64
}

// GetContextUsage computes context usage, or nil when no model/window is known.
func (s *AgentSession) GetContextUsage() *ContextUsageReport {
	model := s.Model()
	if model == nil {
		return nil
	}
	contextWindow := model.ContextWindow
	if contextWindow <= 0 {
		return nil
	}

	// After compaction the last assistant usage reflects the pre-compaction
	// context, so usage is only trusted from an assistant that responded after
	// the latest compaction. Without one the count is unknown.
	latestCompaction := s.Sessions.LatestCompaction()
	if latestCompaction != nil {
		branch := s.Sessions.GetBranch("")
		compactionIndex := -1
		for index, entry := range branch {
			if entry.ID == latestCompaction.ID {
				compactionIndex = index
			}
		}
		projectedAssistants := map[string]bool{}
		for _, entry := range s.Sessions.ProjectedEntries() {
			for _, message := range entry.Messages {
				assistant, ok := message.(*ai.AssistantMessage)
				if ok && assistant.StopReason != ai.StopAborted && assistant.StopReason != ai.StopError &&
					ai.CalculateContextTokens(assistant.Usage) > 0 {
					projectedAssistants[entry.Source.ID] = true
					break
				}
			}
		}
		hasPostCompactionUsage := false
		for index := len(branch) - 1; index > compactionIndex; index-- {
			if projectedAssistants[branch[index].ID] {
				hasPostCompactionUsage = true
				break
			}
		}
		if !hasPostCompactionUsage {
			return &ContextUsageReport{ContextWindow: contextWindow}
		}
	}

	// Context edits change what the model sees without producing usage, so the
	// projection estimate is authoritative when one exists (upstream
	// estimateProjectedContextTokens).
	estimate := s.Sessions.ProjectedContextEstimate()
	tokens := int64(estimate.Tokens)
	percent := float64(estimate.Tokens) / float64(contextWindow) * 100
	return &ContextUsageReport{Tokens: &tokens, ContextWindow: contextWindow, Percent: &percent}
}

// Dispose aborts every active operation and drops listeners.
func (s *AgentSession) Dispose() {
	// Mark the session first: a goal round or released follow-up the host runs
	// while the steps below are still in progress must see it and not start.
	// Upstream's synchronous dispose cannot interleave with them.
	state := s.prompt()
	state.mu.Lock()
	state.disposed = true
	state.mu.Unlock()
	// Upstream wraps only the aborts: "Dispose must succeed even if an abort
	// hook throws". The cleanup below must run either way.
	func() {
		defer func() { _ = recover() }()
		s.AbortAsync()
	}()
	// Upstream _disconnectFromAgent: the aborted run's late events must not
	// reach (and persist into) the disposed session.
	if s.unsubscribeAgent != nil {
		s.unsubscribeAgent()
	}
	if s.CacheWarmer != nil {
		s.CacheWarmer.dispose()
	}
	// The session's resources (temp files, watchers) are released with the
	// session id, like upstream's cleanupSessionResources(this.sessionId).
	// Cleanup is best-effort; the remaining dispose steps still run.
	_ = CleanupSessionResources(s.SessionID())
	s.listenerMu.Lock()
	s.listeners = nil
	s.listenerMu.Unlock()
	// The captured prefix is the disposed session's transcript; nothing will
	// replay it, and it would keep that history alive.
	s.requestPrefix.clear()
}

// SessionManager returns the session manager backing this session.
func (s *AgentSession) SessionManager() *SessionManager { return s.Sessions }

// IsUsingSubscription reports whether a provider is subscription-backed.
func (s *AgentSession) IsUsingSubscription(providerID string) bool {
	if s.control.ModelRuntime == nil {
		return false
	}
	return s.control.ModelRuntime.IsUsingSubscription(providerID)
}
