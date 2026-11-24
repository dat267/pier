package coding

import (
	ctxpkg "context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Round 120 tests: the CreateAgentSession assembly (model restore/fallback,
// thinking-level restore and clamp, tool selection, the block-images filter,
// and the settings-backed request options).

func TestCreateAgentSessionDefaults(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	result, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := result.Session
	if session == nil {
		t.Fatal("no session")
	}
	// The model resolves from the runtime with auth configured... none here, so
	// the fallback message explains.
	if !strings.Contains(result.ModelFallbackMessage, "No models available.") {
		t.Fatalf("fallback = %q", result.ModelFallbackMessage)
	}
	if session.HasModel() {
		t.Fatalf("model = %+v", session.Model())
	}
	if session.ThinkingLevel() != ai.ThinkOff {
		t.Fatalf("thinking level = %q", session.ThinkingLevel())
	}
	// The default tool set is read/bash/edit/write.
	if names := strings.Join(session.GetActiveToolNames(), ","); names != "read,bash,edit,write,set_goal,get_goal" {
		t.Fatalf("tools = %q", names)
	}
	// The initial state is persisted for resume.
	entries := session.Sessions.GetEntries()
	last := entries[len(entries)-1]
	if last.Type != "thinking_level_change" {
		t.Fatalf("last entry = %+v", last)
	}
	foundModelChange := false
	for _, entry := range entries {
		if entry.Type == "model_change" {
			foundModelChange = true
		}
	}
	if foundModelChange {
		t.Fatal("no model change without a model")
	}
	// The cache warmer is wired.
	if session.CacheWarmer == nil {
		t.Fatal("cache warmer missing")
	}
	if status := session.GetCacheWarmingStatus(); status == nil {
		t.Fatal("cache warming status missing")
	}
}

func TestCreateAgentSessionModelResolveAndPersist(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRuntimeAPIKey(ctxpkg.Background(), "anthropic", "sk-1"); err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	// A default model id the catalog does not know resolves to the provider's
	// default model instead.
	settings.SetDefaultProvider("anthropic")
	settings.SetDefaultModel("not-in-catalog")

	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !session.Session.HasModel() {
		t.Fatalf("model = %+v", session.Session.Model())
	}
	resolvedID := session.Session.Model().ID
	if resolvedID == "not-in-catalog" {
		t.Fatalf("model = %+v", session.Session.Model())
	}

	// Restoring from a session's model change entry.
	manager := newTestSessionManager(t)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	manager.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content:    ai.ContentList{ai.TextContent{Text: "reply"}},
		StopReason: ai.StopStop,
	})
	manager.AppendModelChange("anthropic", resolvedID)
	manager.AppendThinkingLevelChange(ai.ThinkHigh)
	restored, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, SessionManager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Session.Model() == nil || restored.Session.Model().ID != resolvedID {
		t.Fatalf("model = %+v", restored.Session.Model())
	}
	if restored.Session.ThinkingLevel() != ai.ThinkHigh {
		t.Fatalf("thinking level = %q", restored.Session.ThinkingLevel())
	}
	// The messages are restored into the agent state.
	if len(restored.Session.Messages()) == 0 {
		t.Fatal("messages not restored")
	}

	// A model reference the runtime cannot serve falls back with a message and
	// then resolves the provider default.
	manager.AppendModelChange("nope", "nope")
	fallback, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, SessionManager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fallback.ModelFallbackMessage, "Could not restore model nope/nope") ||
		!strings.Contains(fallback.ModelFallbackMessage, ". Using anthropic/") {
		t.Fatalf("fallback = %q", fallback.ModelFallbackMessage)
	}
	if !fallback.Session.HasModel() {
		t.Fatalf("model = %+v", fallback.Session.Model())
	}
}

// newReloadSettings builds a settings manager over a writable settings file
// and returns the file's path for later rewrites.
func newReloadSettings(t *testing.T) (*SettingsManager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigDirName, "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeReloadSettings(t, path, `{}`)
	return NewSettingsManagerFromFiles(dir, agentDirForSettings(t), SettingsManagerCreateOptions{}), path
}

func writeReloadSettings(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestToolModifiersRemainAppliedAfterSettingsReload ports reload behavior from
// packages/coding-agent/src/core/agent-session.ts (ddaa0a034).
func TestToolModifiersRemainAppliedAfterSettingsReload(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings, settingsPath := newReloadSettings(t)
	writeReloadSettings(t, settingsPath, `{"defaultTools":["read"]}`)
	settings.Reload()
	extra := agent.AgentTool{Name: "inactive_tool", Label: "inactive", Description: "inactive tool"}
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		ExtraTools: []agent.AgentTool{extra}, Tools: []ToolName{"-bash", "+grep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(session.Session.GetActiveToolNames(), ","); got != "read,grep,set_goal,get_goal" {
		t.Fatalf("initial tools = %q, want read and grep", got)
	}
	writeReloadSettings(t, settingsPath, `{"defaultTools":["read","bash","inactive_tool"]}`)
	session.Session.Reload()
	got := append([]string{}, session.Session.ActiveToolNames()...)
	slices.Sort(got)
	if strings.Join(got, ",") != "get_goal,grep,inactive_tool,read,set_goal" {
		t.Fatalf("tools after reload = %v, want updated defaults with bash still removed", got)
	}
}

// TestReloadActivatesToolsNewlyAddedToDefaultTools ports default-tools-setting.test.ts
// "reload" (db6cc71dc, #10245): /reload enables tools newly added to
// defaultTools; removals stay active, session-disabled tools stay off, and
// explicit --tools/--no-tools/--exclude keep overriding.
func TestReloadActivatesToolsNewlyAddedToDefaultTools(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings, settingsPath := newReloadSettings(t)
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	active := func(s *AgentSession) string {
		names := append([]string{}, s.ActiveToolNames()...)
		slices.Sort(names)
		return strings.Join(names, ",")
	}
	if got := active(session.Session); got != "bash,edit,get_goal,read,set_goal,write" {
		t.Fatalf("initial tools = %q", got)
	}
	// bash disabled during the session stays off unless the setting newly adds it.
	session.Session.SetActiveToolsByName([]string{"read", "edit", "write"})

	writeReloadSettings(t, settingsPath, `{"defaultTools":["+grep"]}`)
	session.Session.Reload()
	if got := active(session.Session); got != "edit,grep,read,write" {
		t.Fatalf("after +grep = %q", got)
	}

	// Removing tools from the setting does not disable them.
	writeReloadSettings(t, settingsPath, `{"defaultTools":["-read"]}`)
	session.Session.Reload()
	if got := active(session.Session); got != "edit,grep,read,write" {
		t.Fatalf("after -read = %q", got)
	}

	// Explicit --tools keeps overriding the setting.
	allowlisted, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, Tools: []ToolName{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeReloadSettings(t, settingsPath, `{"defaultTools":["+grep"]}`)
	allowlisted.Session.Reload()
	if got := strings.Join(allowlisted.Session.ActiveToolNames(), ","); got != "read" {
		t.Fatalf("--tools after reload = %q", got)
	}

	// --no-tools keeps overriding too.
	builtinless, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, NoTools: "builtin",
	})
	if err != nil {
		t.Fatal(err)
	}
	builtinless.Session.Reload()
	if got := builtinless.Session.ActiveToolNames(); len(got) != 0 {
		t.Fatalf("--no-tools after reload = %v", got)
	}

	// CLI exclusions apply to newly added names as well.
	excluded, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, ExcludeTools: []ToolName{"grep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeReloadSettings(t, settingsPath, `{"defaultTools":["+grep"]}`)
	excluded.Session.Reload()
	if got := active(excluded.Session); got != "bash,edit,get_goal,read,set_goal,write" {
		t.Fatalf("excluded after reload = %q", got)
	}
}

// agentDirForSettings gives the settings manager a clean global scope. It is the
// test's own temp dir, so the suite stops leaving one empty /tmp/pi-agent-dir*
// behind per call (492 of them had accumulated).
func agentDirForSettings(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// A reload that activates new defaultTools reads and sets the active set in
// one step: MCP tools attached while it runs stay active. Upstream reload and
// the MCP registry refresh share one event loop (agent-session.ts, pi v1.5.2
// b4afdddf).
func TestReloadKeepsToolsAttachedWhileItRuns(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings, settingsPath := newReloadSettings(t)
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeReloadSettings(t, settingsPath, `{"defaultTools":["+grep"]}`)
	const attaches = 40
	var group sync.WaitGroup
	group.Go(func() {
		for range 20 {
			session.Session.Reload()
		}
	})
	group.Go(func() {
		var tools []agent.AgentTool
		for index := range attaches {
			tools = append(tools, testTool(fmt.Sprintf("mcp__srv__t%d", index)))
			session.Session.AttachExtraTools(append([]agent.AgentTool{}, tools...))
		}
	})
	group.Wait()
	active := 0
	for _, name := range session.Session.ActiveToolNames() {
		if strings.HasPrefix(name, "mcp__") {
			active++
		}
	}
	if active != attaches {
		t.Fatalf("%d of %d attached MCP tools are active after the reloads", active, attaches)
	}
}
