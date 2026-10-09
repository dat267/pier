package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/agent.ts.

type testRegistry struct {
	installed []Extension
	tasks     []Task
}

func newTestRegistry(extensions ...Extension) *testRegistry {
	return &testRegistry{installed: extensions}
}

func (r *testRegistry) Installed() []Extension { return r.installed }

func (r *testRegistry) Extension(name string) *Extension {
	for index := range r.installed {
		if r.installed[index].Name == name {
			return &r.installed[index]
		}
	}
	return nil
}

func (r *testRegistry) Tools() []RegistryTool {
	tools := []RegistryTool{}
	for _, extension := range r.installed {
		for _, tool := range extension.Tools {
			tools = append(tools, RegistryTool{Extension: extension, Tool: tool})
		}
	}
	return tools
}

func (r *testRegistry) Sections() []RegistrySection {
	sections := []RegistrySection{}
	for _, extension := range r.installed {
		for _, section := range extension.Sections {
			sections = append(sections, RegistrySection{Extension: extension, Section: section})
		}
	}
	return sections
}

func (r *testRegistry) Tasks() []Task { return r.tasks }

func (r *testRegistry) Task(name string) *Task {
	for index := range r.tasks {
		if r.tasks[index].Definition.Name == name {
			return &r.tasks[index]
		}
	}
	return nil
}

func testTool(name, description string) ToolRegistration {
	return ToolRegistration{Tool: ai.Tool{Name: name, Description: description}}
}

func textSection(key, text string) PromptSection {
	return PromptSection{Key: key, Render: func(PromptInput, chord.Context) (string, bool, error) {
		return text, true, nil
	}}
}

func intPtr(value int) *int { return &value }

func TestResolveSettings(t *testing.T) {
	defaults := ResolveSettings(nil)
	if !defaults.Retry.Enabled || defaults.Retry.MaxRetries != 3 || defaults.Retry.BaseDelayMs != 2000 ||
		defaults.Retry.MaxAgentDelayMs == nil || *defaults.Retry.MaxAgentDelayMs != 60000 {
		t.Fatalf("retry = %+v", defaults.Retry)
	}
	if defaults.Compaction != DefaultCompactionPolicy {
		t.Fatalf("compaction = %+v", defaults.Compaction)
	}
	if defaults.Progress != (ProgressPolicy{PartialIntervalMs: 100, OutputIntervalMs: 100}) {
		t.Fatalf("progress = %+v", defaults.Progress)
	}
	if defaults.ContextRetentionMs != 600_000 {
		t.Fatalf("contextRetentionMs = %d", defaults.ContextRetentionMs)
	}
	if defaults.ToolExecution != ToolExecutionParallel || defaults.SteeringMode != QueueOneAtATime ||
		defaults.FollowUpMode != QueueOneAtATime {
		t.Fatalf("settings = %+v", defaults)
	}
	if defaults.Extensions != nil {
		t.Fatalf("extensions = %+v", defaults.Extensions)
	}
	// A partial policy overrides only the fields it names.
	partial := ResolveSettings(&HarnessSettings{
		Retry:              &RetryPolicyChange{Enabled: boolPtr(false), MaxRetries: intPtr(9)},
		Compaction:         &CompactionPolicyChange{BackgroundTokens: intPtr(0)},
		Progress:           &ProgressPolicyChange{OutputIntervalMs: intPtr(500)},
		ContextRetentionMs: intPtr(0),
		ToolExecution:      stringPointer(ToolExecutionSequential),
		SteeringMode:       stringPointer(QueueAll),
		Extensions:         []Extension{{Name: "a"}},
	})
	if partial.Retry.Enabled || partial.Retry.MaxRetries != 9 || partial.Retry.BaseDelayMs != 2000 {
		t.Fatalf("retry = %+v", partial.Retry)
	}
	if partial.Compaction != (CompactionPolicy{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000, BackgroundTokens: 0}) {
		t.Fatalf("compaction = %+v", partial.Compaction)
	}
	if partial.Progress != (ProgressPolicy{PartialIntervalMs: 100, OutputIntervalMs: 500}) {
		t.Fatalf("progress = %+v", partial.Progress)
	}
	if partial.ContextRetentionMs != 0 {
		t.Fatalf("contextRetentionMs = %d", partial.ContextRetentionMs)
	}
	if partial.ToolExecution != ToolExecutionSequential || partial.SteeringMode != QueueAll ||
		partial.FollowUpMode != QueueOneAtATime {
		t.Fatalf("settings = %+v", partial)
	}
	if len(partial.Extensions) != 1 || partial.Extensions[0].Name != "a" {
		t.Fatalf("extensions = %+v", partial.Extensions)
	}
}

func boolPtr(value bool) *bool { return &value }

func TestAgentDocShape(t *testing.T) {
	definition := AgentDoc.Definition
	if definition.Kind != "pi.agent" || definition.Version != 1 || definition.Scope != ScopeConversation {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.History == nil || *definition.History != HistoryRewindable ||
		definition.Fork == nil || *definition.Fork != ForkAsOf {
		t.Fatalf("definition = %+v", definition)
	}
	initial, err := definition.Initial(nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := marshalJSONValue(initial)
	if encoded != "{}" {
		t.Fatalf("initial = %s", encoded)
	}
}

func TestApplyChange(t *testing.T) {
	state := map[string]any{"instructions": "old"}
	change := AgentChange{
		Model:         SetOf(ModelRef{Provider: "anthropic", ModelID: "sonnet"}),
		ThinkingLevel: SetOf("high"),
		Extensions:    SetOf(ExtensionsChange{IsList: true, List: []Extension{{Name: "a"}, {Name: "b"}}}),
		Tools: SetOf(ToolsChange{
			IsList: false, Remove: []ToolRegistration{testTool("x", ""), testTool("y", "")},
		}),
	}
	if err := ApplyChange(state, change); err != nil {
		t.Fatal(err)
	}
	encoded, _ := marshalJSONValue(state)
	expected := `{"extensions":["a","b"],"instructions":"old",` +
		`"model":{"modelId":"sonnet","provider":"anthropic"},"thinkingLevel":"high","tools":{"remove":["x","y"]}}`
	// Go marshals map keys sorted; compare decoded structures for the values.
	if encoded != expected {
		// Fall back to a decoded comparison when key order or formatting differs.
		var got, want map[string]any
		_ = json.Unmarshal([]byte(encoded), &got)
		_ = json.Unmarshal([]byte(expected), &want)
		if !jsonEqual(got, want) {
			t.Fatalf("state = %s", encoded)
		}
	}
	// Null clears; absent leaves untouched.
	if err := ApplyChange(state, AgentChange{Instructions: NullOf[string](), Cwd: SetOf("/tmp")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := state["instructions"]; ok {
		t.Fatalf("instructions = %v", state["instructions"])
	}
	if state["cwd"] != "/tmp" {
		t.Fatalf("cwd = %v", state["cwd"])
	}
	if _, ok := state["model"]; !ok {
		t.Fatal("an absent model must not clear the stored one")
	}
}

func TestApplyChangeExtensionsEdit(t *testing.T) {
	state := map[string]any{}
	if err := ApplyChange(state, AgentChange{Extensions: SetOf(ExtensionsChange{
		Add: []Extension{{Name: "a"}}, Remove: []Extension{{Name: "b"}},
	})}); err != nil {
		t.Fatal(err)
	}
	edit, ok := state["extensions"].(map[string]any)
	if !ok || edit["add"] == nil || edit["remove"] == nil {
		t.Fatalf("extensions = %+v", state["extensions"])
	}
}

func TestAddTools(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// An unset filter offers every tool and stays unset.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return AddTools(tx, RootConversationID, []string{"x"})
	}); err != nil {
		t.Fatal(err)
	}
	assertAgentDoc(t, session, nil)
	// An array filter appends names it lacks.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return Configure(tx, RootConversationID, AgentChange{
			Tools: SetOf(ToolsChange{IsList: true, List: []ToolRegistration{testTool("x", "")}}),
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return AddTools(tx, RootConversationID, []string{"x", "y"})
	}); err != nil {
		t.Fatal(err)
	}
	assertAgentDoc(t, session, []any{"x", "y"})
	// A removal filter drops the names it no longer needs to add.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return Configure(tx, RootConversationID, AgentChange{
			Tools: SetOf(ToolsChange{Remove: []ToolRegistration{testTool("a", ""), testTool("b", "")}}),
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return AddTools(tx, RootConversationID, []string{"a"})
	}); err != nil {
		t.Fatal(err)
	}
	assertAgentDoc(t, session, map[string]any{"remove": []any{"b"}})
}

func assertAgentDoc(t *testing.T, session *Session, tools any) {
	t.Helper()
	value, ok, err := session.Snapshot(context.Background(), AgentDoc.Definition, RootConversationID)
	if err != nil || !ok {
		t.Fatalf("snapshot = %v, %v", ok, err)
	}
	object := value.(map[string]any)
	if tools == nil {
		if _, present := object["tools"]; present {
			t.Fatalf("tools = %v", object["tools"])
		}
		return
	}
	if !jsonEqual(object["tools"], tools) {
		t.Fatalf("tools = %#v", object["tools"])
	}
}

func TestCreateAgentCopiesOwner(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		if err != nil {
			return err
		}
		return Configure(tx, RootConversationID, AgentChange{
			Instructions: SetOf("root instructions"),
			Model:        SetOf(ModelRef{Provider: "anthropic", ModelID: "sonnet"}),
		})
	}); err != nil {
		t.Fatal(err)
	}
	var childID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		child, err := tx.CreateConversation(ConversationOwnership{Kind: ConversationOwnerless})
		if err != nil {
			return err
		}
		childID = child.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	taskID := Id(7)
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return CreateAgent(tx, ConversationRecord{
			ID: childID, Owner: &ConversationOwner{ConversationID: RootConversationID, TaskID: taskID},
		})
	}); err != nil {
		t.Fatal(err)
	}
	value, ok, err := session.Snapshot(ctx, AgentDoc.Definition, childID)
	if err != nil || !ok {
		t.Fatalf("snapshot = %v, %v", ok, err)
	}
	object := value.(map[string]any)
	if object["instructions"] != "root instructions" {
		t.Fatalf("agent = %#v", object)
	}
	// A fork keeps its own asOf copy; CreateAgent is a no-op.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return CreateAgent(tx, ConversationRecord{
			ID: childID, Parent: &ConversationParent{ConversationID: RootConversationID, At: 1},
		})
	}); err != nil {
		t.Fatal(err)
	}
	value, _, _ = session.Snapshot(ctx, AgentDoc.Definition, childID)
	if value.(map[string]any)["instructions"] != "root instructions" {
		t.Fatalf("agent = %#v", value)
	}
	// An ownerless new conversation starts empty.
	var siblingID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		sibling, err := tx.CreateConversation(ConversationOwnership{Kind: ConversationOwnerless})
		if err != nil {
			return err
		}
		siblingID = sibling.ID
		return CreateAgent(tx, ConversationRecord{ID: sibling.ID})
	}); err != nil {
		t.Fatal(err)
	}
	value, _, _ = session.Snapshot(ctx, AgentDoc.Definition, siblingID)
	if len(value.(map[string]any)) != 0 {
		t.Fatalf("agent = %#v", value)
	}
}

func TestAgentHooksInExtensionOrder(t *testing.T) {
	agent := Agent{Extensions: []Extension{
		{Name: "a", Hooks: []HookRegistration{{Task: "pi.generation", Handlers: "a1"}, {Task: "other", Handlers: "a2"}}},
		{Name: "b", Hooks: []HookRegistration{{Task: "pi.generation", Handlers: "b1"}}},
	}}
	handlers := AgentHooks(agent, "pi.generation")
	if len(handlers) != 2 || handlers[0] != "a1" || handlers[1] != "b1" {
		t.Fatalf("handlers = %+v", handlers)
	}
}

func TestResolveAgentComposition(t *testing.T) {
	registry := newTestRegistry(
		Extension{Name: "a", Tools: []ToolRegistration{testTool("x", "a-x"), testTool("y", "a-y")},
			Sections: []PromptSection{textSection("s1", "one")}},
		Extension{Name: "b", Tools: []ToolRegistration{testTool("y", "b-y")},
			Sections: []PromptSection{textSection("s2", "two")}},
	)
	settings := Settings{Extensions: registry.Installed()}
	instructions := "be brief"
	state := &AgentState{
		Model:         &ModelRef{Provider: "anthropic", ModelID: "sonnet"},
		ThinkingLevel: stringPointer("high"),
		Tools:         &ToolsSelection{IsList: true, List: []string{"y", "x", "y"}},
		Instructions:  &instructions,
		Cwd:           stringPointer("/work"),
	}
	agent, err := ResolveAgent(state, registry, settings, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Model == nil || agent.Model.ModelID != "sonnet" || agent.ThinkingLevel != "high" || *agent.Cwd != "/work" {
		t.Fatalf("agent = %+v", agent)
	}
	// The later extension overrides y in place, keeping first-seen order.
	if len(agent.Tools) != 2 || agent.Tools[0].Name != "y" || agent.Tools[0].Description != "b-y" || agent.Tools[1].Name != "x" {
		t.Fatalf("tools = %+v", agent.Tools)
	}
	if len(agent.Sections) != 3 || agent.Sections[0].Key != "s1" || agent.Sections[1].Key != "s2" ||
		agent.Sections[2].Key != InstructionsKey {
		t.Fatalf("sections = %+v", agent.Sections)
	}
	text, present, err := agent.Sections[2].Render(PromptInput{}, context.Background())
	if err != nil || !present || text != "be brief" {
		t.Fatalf("instructions = %q, %v, %v", text, present, err)
	}
	// An absent tools filter offers every composed tool.
	plain, err := ResolveAgent(&AgentState{}, registry, settings, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Tools) != 2 || plain.ThinkingLevel != "off" {
		t.Fatalf("plain = %+v", plain)
	}
	// A removal filter drops by name.
	filtered, err := ResolveAgent(&AgentState{Tools: &ToolsSelection{Remove: []string{"x"}}}, registry, settings, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Tools) != 1 || filtered.Tools[0].Name != "y" {
		t.Fatalf("filtered = %+v", filtered.Tools)
	}
	// A nil state resolves to every installed extension.
	none, err := ResolveAgent(nil, registry, settings, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Extensions) != 2 || len(none.Tools) != 2 {
		t.Fatalf("none = %+v", none)
	}
}

func TestResolveAgentWraps(t *testing.T) {
	var reported []error
	wrapped := testTool("x", "wrapped")
	registry := newTestRegistry(Extension{
		Name: "a",
		Tools: []ToolRegistration{
			testTool("x", "original"),
			testTool("y", "y"),
		},
		Sections: []PromptSection{textSection("s1", "one"), textSection("s2", "two")},
		Wraps: []Wrap{
			{Tool: "x", WrapTool: func(ToolRegistration) ToolRegistration { return wrapped }},
			{Tool: "y", WrapTool: func(tool ToolRegistration) ToolRegistration {
				tool.Name = "renamed"
				return tool
			}},
			{Section: "s1", WrapSection: func(PromptSection) PromptSection { return textSection("s1", "wrapped") }},
		},
	})
	agent, err := ResolveAgent(nil, registry, Settings{Extensions: registry.Installed()}, func(err error) {
		reported = append(reported, err)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Tools) != 1 || agent.Tools[0].Name != "x" || agent.Tools[0].Description != "wrapped" {
		t.Fatalf("tools = %+v", agent.Tools)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %+v", reported)
	}
	text, _, _ := agent.Sections[0].Render(PromptInput{}, context.Background())
	if agent.Sections[0].Key != "s1" || text != "wrapped" {
		t.Fatalf("section = %+v", agent.Sections[0])
	}
}

func TestSelectExtensions(t *testing.T) {
	registry := newTestRegistry(
		Extension{Name: "a"}, Extension{Name: "b"}, Extension{Name: "c"},
	)
	// The default is every installed extension.
	all := SelectExtensions(nil, registry, Settings{})
	if len(all) != 3 {
		t.Fatalf("all = %+v", all)
	}
	// Settings replace the default selection.
	fromSettings := SelectExtensions(nil, registry, Settings{Extensions: []Extension{{Name: "b"}}})
	if len(fromSettings) != 1 || fromSettings[0].Name != "b" {
		t.Fatalf("fromSettings = %+v", fromSettings)
	}
	// A stored array selects exactly those names, deduped, unknown dropped.
	listed := SelectExtensions(&AgentState{Extensions: &ExtensionsSelection{IsList: true, List: []string{"c", "missing", "c"}}},
		registry, Settings{})
	if len(listed) != 1 || listed[0].Name != "c" {
		t.Fatalf("listed = %+v", listed)
	}
	// An edit adds to and removes from the default selection.
	edited := SelectExtensions(&AgentState{Extensions: &ExtensionsSelection{Add: []string{"c"}, Remove: []string{"a"}}},
		registry, Settings{})
	if len(edited) != 2 || edited[0].Name != "b" || edited[1].Name != "c" {
		t.Fatalf("edited = %+v", edited)
	}
}

func jsonEqual(left, right any) bool {
	leftEncoded, _ := json.Marshal(left)
	rightEncoded, _ := json.Marshal(right)
	return string(leftEncoded) == string(rightEncoded)
}
