package durable

import (
	"fmt"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/agent.ts: the built-in agent document and its resolution.

// DefaultRetryPolicy is the built-in retry policy.
var DefaultRetryPolicy = ConversationRetryPolicy{
	Enabled: true, MaxRetries: 3, BaseDelayMs: 2000, MaxAgentDelayMs: intPointer(60000),
}

// DefaultCompactionPolicy is the built-in compaction policy.
var DefaultCompactionPolicy = CompactionPolicy{
	Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000, BackgroundTokens: 32768,
}

// InstructionsKey is the reserved section key of the agent's instructions.
const InstructionsKey = "instructions"

// AgentDoc is the built-in agent document; rewindable so forks start from the
// agent at their fork entry.
var AgentDoc = mustDefineDoc(DocDefinition{
	Kind: "pi.agent", Version: 1, Scope: ScopeConversation,
	History: stringPointer(HistoryRewindable), Fork: stringPointer(ForkAsOf),
	Initial:        func(chord.JsonValue) (chord.JsonValue, error) { return map[string]any{}, nil },
	CheckpointWhen: func(chord.JsonValue, []delta.Op, CheckpointInfo) bool { return true },
})

// ResolveSettings resolves the host settings: every field over its built-in
// default, object fields merged.
func ResolveSettings(settings *HarnessSettings) Settings {
	resolved := Settings{
		Stream: ConversationStreamOptions{}, Retry: DefaultRetryPolicy,
		Compaction: DefaultCompactionPolicy, ToolExecution: ToolExecutionParallel,
		SteeringMode: QueueOneAtATime, FollowUpMode: QueueOneAtATime,
	}
	if settings == nil {
		return resolved
	}
	resolved.Extensions = settings.Extensions
	if settings.Stream != nil {
		resolved.Stream = *settings.Stream
	}
	if settings.Retry != nil {
		if settings.Retry.Enabled != nil {
			resolved.Retry.Enabled = *settings.Retry.Enabled
		}
		if settings.Retry.MaxRetries != nil {
			resolved.Retry.MaxRetries = *settings.Retry.MaxRetries
		}
		if settings.Retry.BaseDelayMs != nil {
			resolved.Retry.BaseDelayMs = *settings.Retry.BaseDelayMs
		}
		if settings.Retry.MaxAgentDelayMs != nil {
			resolved.Retry.MaxAgentDelayMs = settings.Retry.MaxAgentDelayMs
		}
	}
	if settings.Compaction != nil {
		if settings.Compaction.Enabled != nil {
			resolved.Compaction.Enabled = *settings.Compaction.Enabled
		}
		if settings.Compaction.ReserveTokens != nil {
			resolved.Compaction.ReserveTokens = *settings.Compaction.ReserveTokens
		}
		if settings.Compaction.KeepRecentTokens != nil {
			resolved.Compaction.KeepRecentTokens = *settings.Compaction.KeepRecentTokens
		}
		if settings.Compaction.BackgroundTokens != nil {
			resolved.Compaction.BackgroundTokens = *settings.Compaction.BackgroundTokens
		}
	}
	if settings.ToolExecution != nil {
		resolved.ToolExecution = *settings.ToolExecution
	}
	if settings.SteeringMode != nil {
		resolved.SteeringMode = *settings.SteeringMode
	}
	if settings.FollowUpMode != nil {
		resolved.FollowUpMode = *settings.FollowUpMode
	}
	return resolved
}

// Configure applies one change to `pi.agent` in the caller's transaction.
func Configure(tx *Transaction, conversationID Id, change AgentChange) error {
	state, err := tx.Doc(AgentDoc.Definition, conversationID)
	if err != nil {
		return err
	}
	return ApplyChange(state, change)
}

// ApplyChange mutates a `pi.agent` draft: a given field replaces the stored
// one, null clears it, absent changes nothing.
func ApplyChange(state chord.JsonValue, change AgentChange) error {
	object, ok := state.(map[string]any)
	if !ok {
		return fmt.Errorf("agent state is not an object")
	}
	set := func(key string, field Optional[string]) {
		if !field.Set {
			return
		}
		if field.Null {
			delete(object, key)
			return
		}
		object[key] = field.Value
	}
	if change.Model.Set {
		if change.Model.Null {
			delete(object, "model")
		} else {
			object["model"] = map[string]any{"provider": change.Model.Value.Provider, "modelId": change.Model.Value.ModelID}
		}
	}
	set("thinkingLevel", change.ThinkingLevel)
	if change.Extensions.Set {
		if change.Extensions.Null {
			delete(object, "extensions")
		} else {
			extensions := change.Extensions.Value
			if extensions.IsList {
				object["extensions"] = extensionNames(extensions.List)
			} else {
				edit := map[string]any{}
				if extensions.Add != nil {
					edit["add"] = extensionNames(extensions.Add)
				}
				if extensions.Remove != nil {
					edit["remove"] = extensionNames(extensions.Remove)
				}
				object["extensions"] = edit
			}
		}
	}
	if change.Tools.Set {
		if change.Tools.Null {
			delete(object, "tools")
		} else {
			tools := change.Tools.Value
			if tools.IsList {
				object["tools"] = toolNames(tools.List)
			} else {
				object["tools"] = map[string]any{"remove": toolNames(tools.Remove)}
			}
		}
	}
	set("instructions", change.Instructions)
	set("cwd", change.Cwd)
	return nil
}

// AddTools appends names an array tool filter lacks, or drops them from a
// `{ remove }` filter; an unset filter offers every tool and stays unset.
func AddTools(tx *Transaction, conversationID Id, added []string) error {
	state, err := tx.Doc(AgentDoc.Definition, conversationID)
	if err != nil {
		return err
	}
	object, ok := state.(map[string]any)
	if !ok {
		return fmt.Errorf("agent state is not an object")
	}
	tools, present := object["tools"]
	if !present || tools == nil {
		return nil
	}
	if list, isList := asAnySlice(tools); isList {
		for _, name := range added {
			if !stringSliceContains(list, name) {
				list = append(list, name)
			}
		}
		object["tools"] = list
		return nil
	}
	if filter, isObject := tools.(map[string]any); isObject {
		remove, _ := asAnySlice(filter["remove"])
		overlap := false
		for _, name := range added {
			if stringSliceContains(remove, name) {
				overlap = true
				break
			}
		}
		if overlap {
			filtered := []any{}
			for _, name := range remove {
				value, _ := name.(string)
				if !stringInSlice(added, value) {
					filtered = append(filtered, name)
				}
			}
			object["tools"] = map[string]any{"remove": filtered}
		}
	}
	return nil
}

// CreateAgent is the built-in part of every commit that creates or forks a
// conversation: a new task-owned conversation copies the stored agent of its
// owner task's conversation; a new ownerless one starts empty.
func CreateAgent(tx *Transaction, conversation ConversationRecord) error {
	if conversation.Parent != nil {
		return nil
	}
	agent, err := tx.Doc(AgentDoc.Definition, conversation.ID)
	if err != nil {
		return err
	}
	if conversation.Owner == nil {
		return nil
	}
	owner, err := tx.Doc(AgentDoc.Definition, conversation.Owner.ConversationID)
	if err != nil {
		return err
	}
	target, ok := agent.(map[string]any)
	if !ok {
		return fmt.Errorf("agent state is not an object")
	}
	source, ok := owner.(map[string]any)
	if !ok {
		return nil
	}
	for key, value := range source {
		target[key] = deepCopyJSON(value)
	}
	return nil
}

// AgentHooks is the selected extensions' handlers for a task name, in
// extension order.
func AgentHooks(agent Agent, taskName string) []any {
	handlers := []any{}
	for _, extension := range agent.Extensions {
		for _, hook := range extension.Hooks {
			if hook.Task == taskName {
				handlers = append(handlers, hook.Handlers)
			}
		}
	}
	return handlers
}

// ResolveAgent resolves an agent from its stored state (absent: every field
// unset), a registry snapshot, and resolved settings. A wrapper that panics or
// renames drops its target and is reported.
func ResolveAgent(state *AgentState, snapshot RegistrySnapshot, settings Settings, report func(error)) (Agent, error) {
	extensions := SelectExtensions(state, snapshot, settings)

	composed := newOrderedMap[ToolRegistration]()
	for _, extension := range extensions {
		for _, tool := range extension.Tools {
			composed.Set(tool.Name, tool)
		}
	}
	sections := newOrderedMap[PromptSection]()
	for _, extension := range extensions {
		for _, section := range extension.Sections {
			sections.Set(section.Key, section)
		}
	}
	for _, extension := range extensions {
		for _, wrap := range extension.Wraps {
			if wrap.Tool != "" {
				applyWrap(composed, wrap.Tool, wrap.WrapTool, func(tool ToolRegistration) string { return tool.Name }, report)
			} else {
				applyWrap(sections, wrap.Section, wrap.WrapSection, func(section PromptSection) string { return section.Key }, report)
			}
		}
	}

	var tools []ToolRegistration
	if state == nil || state.Tools == nil {
		tools = composed.Values()
	} else if state.Tools.IsList {
		tools = []ToolRegistration{}
		seen := map[string]bool{}
		for _, name := range state.Tools.List {
			if seen[name] {
				continue
			}
			seen[name] = true
			if tool, ok := composed.Get(name); ok {
				tools = append(tools, tool)
			}
		}
	} else {
		removed := map[string]bool{}
		for _, name := range state.Tools.Remove {
			removed[name] = true
		}
		tools = []ToolRegistration{}
		for _, tool := range composed.Values() {
			if !removed[tool.Name] {
				tools = append(tools, tool)
			}
		}
	}

	agentSections := sections.Values()
	agent := Agent{ThinkingLevel: "off", Extensions: extensions, Tools: tools, Sections: agentSections}
	if state == nil {
		return agent, nil
	}
	agent.Model = state.Model
	if state.ThinkingLevel != nil {
		agent.ThinkingLevel = *state.ThinkingLevel
	}
	if state.Instructions != nil {
		agent.Instructions = state.Instructions
		instructions := *state.Instructions
		agentSections = append(agentSections, PromptSection{
			Key: InstructionsKey,
			Render: func(PromptInput, chord.Context) (string, bool, error) {
				return instructions, true, nil
			},
		})
		agent.Sections = agentSections
	}
	agent.Cwd = state.Cwd
	return agent, nil
}

// SelectExtensions is the installed extensions the state selects: the stored
// array, or the default selection edited by `{ add, remove }`.
func SelectExtensions(state *AgentState, snapshot RegistrySnapshot, settings Settings) []Extension {
	var selected []string
	if state != nil && state.Extensions != nil && state.Extensions.IsList {
		selected = state.Extensions.List
	} else {
		var base []string
		if settings.Extensions != nil {
			base = extensionNames(settings.Extensions)
		} else {
			base = extensionNames(snapshot.Installed())
		}
		selected = append(selected, base...)
		var edit *ExtensionsSelection
		if state != nil {
			edit = state.Extensions
		}
		if edit != nil {
			selected = append(selected, edit.Add...)
		}
		removed := map[string]bool{}
		if edit != nil {
			for _, name := range edit.Remove {
				removed[name] = true
			}
		}
		filtered := []string{}
		for _, name := range selected {
			if !removed[name] {
				filtered = append(filtered, name)
			}
		}
		selected = filtered
	}
	extensions := []Extension{}
	seen := map[string]bool{}
	for _, name := range selected {
		if seen[name] {
			continue
		}
		seen[name] = true
		if extension := snapshot.Extension(name); extension != nil {
			extensions = append(extensions, *extension)
		}
	}
	return extensions
}

// applyWrap replaces a tool or section in place; a nil target does nothing.
func applyWrap[T any](items *orderedMap[T], target string, wrap func(T) T, nameOf func(T) string, report func(error)) {
	item, ok := items.Get(target)
	if !ok {
		return
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				items.Delete(target)
				report(fmt.Errorf("%v", recovered))
			}
		}()
		wrapped := wrap(item)
		if nameOf(wrapped) != target {
			panic(fmt.Sprintf("Wrapper renamed %s to %s", target, nameOf(wrapped)))
		}
		items.Set(target, wrapped)
	}()
}

// orderedMap is a string-keyed map with JS Map insertion order.
type orderedMap[T any] struct {
	order []string
	items map[string]T
}

func newOrderedMap[T any]() *orderedMap[T] {
	return &orderedMap[T]{items: map[string]T{}}
}

func (m *orderedMap[T]) Set(key string, value T) {
	if _, ok := m.items[key]; !ok {
		m.order = append(m.order, key)
	}
	m.items[key] = value
}

func (m *orderedMap[T]) Get(key string) (T, bool) {
	value, ok := m.items[key]
	return value, ok
}

func (m *orderedMap[T]) Delete(key string) {
	if _, ok := m.items[key]; !ok {
		return
	}
	delete(m.items, key)
	for index, name := range m.order {
		if name == key {
			m.order = append(m.order[:index], m.order[index+1:]...)
			break
		}
	}
}

func (m *orderedMap[T]) Keys() []string {
	return append([]string{}, m.order...)
}

func (m *orderedMap[T]) Values() []T {
	values := make([]T, 0, len(m.order))
	for _, key := range m.order {
		values = append(values, m.items[key])
	}
	return values
}

func extensionNames(extensions []Extension) []string {
	names := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		names = append(names, extension.Name)
	}
	return names
}

func toolNames(tools []ToolRegistration) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// asAnySlice normalizes a JSON array that may be typed Go slice.
func asAnySlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		values := make([]any, len(typed))
		for index, item := range typed {
			values[index] = item
		}
		return values, true
	default:
		return nil, false
	}
}

func stringSliceContains(values []any, target string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == target {
			return true
		}
	}
	return false
}

func stringInSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// deepCopyJSON clones a JSON value.
func deepCopyJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, item := range typed {
			clone[key] = deepCopyJSON(item)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = deepCopyJSON(item)
		}
		return clone
	default:
		return value
	}
}

func intPointer(value int) *int { return &value }
