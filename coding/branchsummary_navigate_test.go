package coding

import (
	ctxpkg "context"
	"encoding/json"
	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"strings"
	"testing"
)

// Round 106 tests: branch summarization and session tree navigation.

func sessionWithBranch(t *testing.T) *SessionManager {
	t.Helper()
	manager := newTestSessionManager(t)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "start"}})
	parent := manager.GetEntries()[0].ID
	manager.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{
			ai.TextContent{Text: "reading"},
			ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"/tmp/a.txt"}`)},
		},
		StopReason: ai.StopStop,
	})
	manager.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "call-1", ToolName: "read", Content: ai.UserContentList{ai.TextContent{Text: "file body"}},
	})
	manager.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{
			ai.ToolCall{ID: "call-2", Name: "edit", Arguments: json.RawMessage(`{"path":"/tmp/b.txt"}`)},
		},
		StopReason: ai.StopStop,
	})
	entries := manager.GetEntries()
	_ = entries
	_ = parent
	return manager
}

func TestNavigateTree(t *testing.T) {
	manager := sessionWithBranch(t)
	entries := manager.GetEntries()
	rootID := entries[0].ID
	leafID := entries[len(entries)-1].ID

	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager,
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
		StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.control = &AgentSessionControl{}

	// Navigating to the current leaf is a no-op.
	result, err := session.NavigateTree(ctxpkg.Background(), leafID, NavigateTreeOptions{})
	if err != nil || result.Cancelled || result.EditorText != "" {
		t.Fatalf("result = %+v err = %v", result, err)
	}

	// Navigating to the root user message resets the leaf and returns the text.
	result, err = session.NavigateTree(ctxpkg.Background(), rootID, NavigateTreeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.EditorText != "start" {
		t.Fatalf("editorText = %q", result.EditorText)
	}
	if leaf := session.Sessions.GetLeafID(); leaf != nil {
		t.Fatalf("leaf = %v", *leaf)
	}

	// An unknown target is an error.
	if _, err := session.NavigateTree(ctxpkg.Background(), "missing", NavigateTreeOptions{}); err == nil ||
		!strings.Contains(err.Error(), "Entry missing not found") {
		t.Fatalf("err = %v", err)
	}

	// Summarizing requires a model.
	modelless, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	modelless.control = &AgentSessionControl{}
	if _, err := modelless.NavigateTree(ctxpkg.Background(), entries[1].ID, NavigateTreeOptions{Summarize: true}); err == nil ||
		err.Error() != "No model available for summarization" {
		t.Fatalf("err = %v", err)
	}
}

func TestNavigateTreeWithSummary(t *testing.T) {
	manager := sessionWithBranch(t)
	entries := manager.GetEntries()
	rootID := entries[0].ID
	leafID := entries[len(entries)-1].ID

	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager,
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
		StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.control = &AgentSessionControl{}
	session.CompactionStreamFn = func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				Content:    ai.ContentList{ai.TextContent{Text: "branch summary body"}},
				StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}

	result, err := session.NavigateTree(ctxpkg.Background(), rootID, NavigateTreeOptions{
		Summarize: true, Label: "explored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SummaryEntry == nil || !strings.Contains(result.SummaryEntry.Summary, "branch summary body") {
		t.Fatalf("summaryEntry = %+v", result.SummaryEntry)
	}
	// The summary records the branch position it left.
	if result.SummaryEntry.FromID != leafID {
		t.Fatalf("summaryEntry = %+v (wanted fromId %s)", result.SummaryEntry, leafID)
	}
	// The details carry the file lists.
	var details BranchSummaryDetails
	if err := json.Unmarshal(result.SummaryEntry.Details, &details); err != nil {
		t.Fatal(err)
	}
	if strings.Join(details.ReadFiles, ",") != "/tmp/a.txt" {
		t.Fatalf("details = %+v", details)
	}
	// A label entry follows the summary.
	after := session.Sessions.GetEntries()
	last := after[len(after)-1]
	if last.Type != "label" || last.Label == nil || *last.Label != "explored" {
		t.Fatalf("last = %+v", last)
	}

	// Without summarize, the label attaches to the target entry.
	if _, err := session.NavigateTree(ctxpkg.Background(), entries[1].ID, NavigateTreeOptions{Label: "target"}); err != nil {
		t.Fatal(err)
	}
	after = session.Sessions.GetEntries()
	last = after[len(after)-1]
	if last.Type != "label" || last.TargetID != entries[1].ID {
		t.Fatalf("last = %+v", last)
	}
}

// pi v1.4.0 defaults reserveTokens only when it is omitted; an explicit zero
// in branchSummary settings leaves the entire model context available.
func TestNavigateTreeHonorsZeroBranchSummaryReserve(t *testing.T) {
	manager := sessionWithBranch(t)
	rootID := manager.GetEntries()[0].ID
	const longBranchText = "This branch text must remain in summary context. "
	longText := strings.Repeat(longBranchText, 500)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: longText}})

	zero := int64(0)
	settings := NewInMemorySettingsManager(&Settings{
		BranchSummary: &SettingsBranchSummary{ReserveTokens: &zero},
	}, SettingsManagerCreateOptions{})
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager,
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 20000, MaxTokens: 8192},
		StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.control = &AgentSessionControl{Settings: settings}

	calls := 0
	included := false
	session.CompactionStreamFn = func(model *ai.Model, context ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		for _, message := range context.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				included = strings.Contains(contentTextJoinedNoSep(user.Content), longBranchText)
			}
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "summary"}}, StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}

	if _, err := session.NavigateTree(ctxpkg.Background(), rootID, NavigateTreeOptions{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !included {
		t.Fatalf("summary calls = %d, long branch text included = %v; want one call with the text", calls, included)
	}
}

func TestRestoreToolsFromTranscript(t *testing.T) {
	manager := newTestSessionManager(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager,
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
		StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.control = &AgentSessionControl{Tools: map[string]AgentToolDefinition{
		"read": {Tool: testTool("read")},
		"bash": {Tool: testTool("bash")},
	}}
	// The transcript's current system message declares the tools.
	prompt := &ai.SystemMessage{
		Content:    ai.StringOrBlocks{Text: "system"},
		ToolsAdded: []ai.Tool{{Name: "read", Description: "read", Parameters: json.RawMessage(`{}`)}, {Name: "unknown", Parameters: json.RawMessage(`{}`)}},
	}
	manager.AppendMessage(prompt)
	session.RestoreToolsFromTranscript()
	if names := session.GetActiveToolNames(); strings.Join(names, ",") != "read" {
		t.Fatalf("active = %v", names)
	}

	// With no system message in the transcript nothing changes.
	empty := newTestSessionManager(t)
	session2, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: empty, Model: session.Model(), StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	session2.control = &AgentSessionControl{Tools: map[string]AgentToolDefinition{"read": {Tool: testTool("read")}}}
	session2.Agent.SetTools([]agent.AgentTool{testTool("read")})
	session2.RestoreToolsFromTranscript()
	if names := session2.GetActiveToolNames(); len(names) != 1 {
		t.Fatalf("active = %v", names)
	}
}

func TestNewSessionRestoresToolLoadoutFromTranscript(t *testing.T) {
	// Upstream 6f2f164ab: a default-tools session restores the tool loadout its
	// transcript records once the registry is built, with modifier-only --tools
	// entries applied. Before the fix the constructor only restored when the
	// caller passed no initial tools, which the SDK never does.
	transcript := func(t *testing.T) *SessionManager {
		t.Helper()
		manager := newTestSessionManager(t)
		manager.AppendMessage(&ai.SystemMessage{
			Content:    ai.StringOrBlocks{Text: "system"},
			ToolsAdded: []ai.Tool{{Name: "read", Parameters: json.RawMessage(`{}`)}, {Name: "grep", Parameters: json.RawMessage(`{}`)}},
		})
		return manager
	}
	registry := func() *AgentSessionControl {
		return &AgentSessionControl{Tools: map[string]AgentToolDefinition{
			"read": {Tool: testTool("read")},
			"bash": {Tool: testTool("bash")},
			"grep": {Tool: testTool("grep")},
		}}
	}
	newSession := func(t *testing.T, usesDefault bool, modifiers []string) *AgentSession {
		t.Helper()
		session, err := NewAgentSession(&SessionConfig{
			Cwd: t.TempDir(), Sessions: transcript(t),
			Model:                &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
			StreamFn:             stubStreamFn,
			Tools:                []agent.AgentTool{testTool("read"), testTool("bash")},
			UsesDefaultTools:     usesDefault,
			DefaultToolModifiers: modifiers,
			Control:              registry(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}

	// A default-tools session keeps the transcript loadout.
	session := newSession(t, true, nil)
	if names := session.GetActiveToolNames(); strings.Join(names, ",") != "read,grep" {
		t.Fatalf("active = %v; want read,grep", names)
	}
	// Modifier-only --tools entries apply to the restored loadout; an unknown
	// +name is dropped by the registry check.
	session = newSession(t, true, []string{"-grep", "+edit"})
	if names := session.GetActiveToolNames(); strings.Join(names, ",") != "read" {
		t.Fatalf("active = %v; want read", names)
	}
	// An explicit tool list does not restore the transcript loadout.
	session = newSession(t, false, nil)
	if names := session.GetActiveToolNames(); strings.Join(names, ",") != "read,bash" {
		t.Fatalf("active = %v; want read,bash", names)
	}
}

func TestResumeRestoreKeepsMCPToolsAttachable(t *testing.T) {
	// A transcript loadout can name MCP tools, but the registry does not hold
	// them at construction (the MCP manager attaches them later). The restore
	// drops them for now; AttachExtraTools re-adds them on top of the restored
	// built-ins, so a resumed session still gets its MCP tools.
	manager := newTestSessionManager(t)
	manager.AppendMessage(&ai.SystemMessage{
		Content: ai.StringOrBlocks{Text: "system"},
		ToolsAdded: []ai.Tool{
			{Name: "read", Parameters: json.RawMessage(`{}`)},
			{Name: "mcp__docs__echo", Parameters: json.RawMessage(`{}`)},
		},
	})
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager,
		Model:            &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
		StreamFn:         stubStreamFn,
		Tools:            []agent.AgentTool{testTool("read"), testTool("bash")},
		UsesDefaultTools: true,
		Control: &AgentSessionControl{Tools: map[string]AgentToolDefinition{
			"read": {Tool: testTool("read")},
			"bash": {Tool: testTool("bash")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := session.GetActiveToolNames(); strings.Join(names, ",") != "read" {
		t.Fatalf("active before MCP attach = %v; want read", names)
	}
	session.AttachExtraTools([]agent.AgentTool{testTool("mcp__docs__echo")})
	if names := session.GetActiveToolNames(); strings.Join(names, ",") != "read,mcp__docs__echo" {
		t.Fatalf("active after MCP attach = %v; want read,mcp__docs__echo", names)
	}
}

// tempAgentDir points the agent dir at a temp directory for the test, so
// persisted session files never land in the repository or home.
func tempAgentDir(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
}

// newTestSessionManager builds a persisted session manager in a temp cwd.
func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	tempAgentDir(t)
	return NewSessionManager(t.TempDir(), nil)
}

// TestNavigateTreeAbortBranchSummary covers the branch-summary abort
// controller: navigation reports IsCompacting and AbortBranchSummary cancels
// the in-flight summary.
func TestNavigateTreeAbortBranchSummary(t *testing.T) {
	manager := sessionWithBranch(t)
	entries := manager.GetEntries()
	rootID := entries[0].ID

	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager,
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
		StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.control = &AgentSessionControl{}

	started := make(chan struct{})
	session.CompactionStreamFn = func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		close(started)
		go func() {
			<-options.Ctx.Done()
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				StopReason: ai.StopAborted,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopAborted, Message: message})
		}()
		return stream
	}

	type outcome struct {
		result *NavigateTreeResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := session.NavigateTree(ctxpkg.Background(), rootID, NavigateTreeOptions{Summarize: true})
		done <- outcome{result, err}
	}()

	<-started
	if !session.IsCompacting() {
		t.Fatal("navigation must report as compacting")
	}
	session.AbortBranchSummary()

	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result == nil || !got.result.Aborted || !got.result.Cancelled {
		t.Fatalf("result = %+v", got.result)
	}
	if session.IsCompacting() {
		t.Fatal("still compacting after navigation finished")
	}
}

// TestFreshSessionPersistsItsInitialToolDeclaration covers the resume half of a
// fresh default-tools session: the agent seeds the initial tool declaration
// into state but never emits it, so the transcript only held the later deltas
// (goal/MCP tools). A resume restores the loadout from the transcript
// (upstream 6f2f164ab), so it resolved no built-in tools. The constructor now
// persists the initial declaration for a fresh session.
func TestFreshSessionPersistsItsInitialToolDeclaration(t *testing.T) {
	manager := newTestSessionManager(t)
	tools := []agent.AgentTool{
		{Name: "read", Description: "read", Parameters: []byte(`{"type":"object"}`)},
		{Name: "bash", Description: "bash", Parameters: []byte(`{"type":"object"}`)},
	}
	registry := func() *AgentSessionControl {
		return &AgentSessionControl{Tools: map[string]AgentToolDefinition{
			"read": {Tool: tools[0]},
			"bash": {Tool: tools[1]},
		}}
	}
	newSession := func() *AgentSession {
		session, err := NewAgentSession(&SessionConfig{
			Cwd: t.TempDir(), Sessions: manager,
			Model:            &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
			StreamFn:         stubStreamFn,
			Tools:            tools,
			UsesDefaultTools: true,
			Control:          registry(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}

	first := newSession()
	// The declaration is persisted at construction, before the turn runs.
	entries := manager.GetEntries()
	if len(entries) == 0 || entries[0].Type != "message" {
		t.Fatalf("first entry = %+v; want the initial declaration message", entries)
	}
	var declaration struct {
		Role       string `json:"role"`
		ToolsAdded []struct {
			Name string `json:"name"`
		} `json:"toolsAdded"`
	}
	if err := json.Unmarshal(entries[0].Message, &declaration); err != nil {
		t.Fatal(err)
	}
	if declaration.Role != "system" || len(declaration.ToolsAdded) != 2 {
		t.Fatalf("first entry = role %q tools %v; want a system message declaring read,bash", declaration.Role, declaration.ToolsAdded)
	}
	if err := first.PromptText(ctxpkg.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if names := first.GetActiveToolNames(); strings.Join(names, ",") != "read,bash" {
		t.Fatalf("active after the run = %v; want read,bash", names)
	}

	resumed := newSession()
	if names := resumed.GetActiveToolNames(); strings.Join(names, ",") != "read,bash" {
		t.Fatalf("resumed active = %v; want read,bash", names)
	}
}
