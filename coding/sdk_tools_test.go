package coding

import (
	ctxpkg "context"
	"github.com/dat267/pier/agent"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Round 120 tests: the CreateAgentSession assembly (model restore/fallback,
// thinking-level restore and clamp, tool selection, the block-images filter,
// and the settings-backed request options).

// TestCreateAgentSessionToolsModifierAddsToDefaults ports modifier-only
// `--tools` selections from packages/coding-agent/src/core/sdk.ts (ddaa0a034).
func TestCreateAgentSessionToolsModifierAddsToDefaults(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"+grep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(session.Session.GetActiveToolNames(), ",")
	if got != "read,bash,edit,write,grep,set_goal,get_goal" {
		t.Fatalf("tools = %q, want defaults with grep added", got)
	}

	withoutDefaults, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		NoTools: NoToolsAll, Tools: []ToolName{"+grep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(withoutDefaults.Session.GetActiveToolNames(), ","); got != "grep" {
		t.Fatalf("tools with --no-tools and +grep = %q, want only grep", got)
	}
}

// TestCreateAgentSessionToolsModifierRemovesFromDefaults follows the same
// upstream modifier contract in packages/coding-agent/src/core/sdk.ts.
func TestCreateAgentSessionToolsModifierRemovesFromDefaults(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"-write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(session.Session.GetActiveToolNames(), ",")
	if got != "read,bash,edit,set_goal,get_goal" {
		t.Fatalf("tools = %q, want write removed from defaults", got)
	}
}

// TestCreateAgentSessionRejectsMixedToolModifiers follows validation in
// packages/coding-agent/src/core/sdk.ts (ddaa0a034).
func TestCreateAgentSessionRejectsMixedToolModifiers(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	_, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"read", "+grep"},
	})
	if err == nil || !strings.Contains(err.Error(), "tool names cannot be mixed") {
		t.Fatalf("err = %v, want mixed modifier validation error", err)
	}
}

func TestCreateAgentSessionToolSelection(t *testing.T) {
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
	cwd := t.TempDir()

	// The allowed list wins.
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings, Tools: []ToolName{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read" {
		t.Fatalf("tools = %q", names)
	}

	// Caller-supplied tools (MCP) join the registry and follow the same
	// selection by name.
	extra := agent.AgentTool{Name: "mcp__docs__search", Label: "docs/search", Description: "Docs search"}
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings,
		ExtraTools: []agent.AgentTool{extra}, Tools: []ToolName{"read", "mcp__docs__search"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read,mcp__docs__search" {
		t.Fatalf("extra tools = %q", names)
	}
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings,
		ExtraTools: []agent.AgentTool{extra}, Tools: []ToolName{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read" {
		t.Fatalf("unselected extra tool leaked: %q", names)
	}

	// noTools disables everything.
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings, NoTools: "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Session.GetActiveToolNames()) != 0 {
		t.Fatalf("tools = %v", session.Session.GetActiveToolNames())
	}

	// Exclusions apply to the defaults.
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings, ExcludeTools: []ToolName{"bash", "write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read,edit,set_goal,get_goal" {
		t.Fatalf("tools = %q", names)
	}

	// The configured default tools are honored (written through the settings
	// file: defaultTools).
	configuredDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configuredDir, ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuredDir, ConfigDirName, "settings.json"),
		[]byte(`{"defaultTools":["read","grep"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configured := NewSettingsManagerFromFiles(configuredDir, agentDirForSettings(t), SettingsManagerCreateOptions{})
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: configuredDir, ModelRuntime: runtime, SettingsManager: configured,
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read,grep,set_goal,get_goal" {
		t.Fatalf("tools = %q", names)
	}

	// Modifier-only defaultTools layer on the built-in selection: +grep adds,
	// -write removes (upstream default-tools-setting.test.ts, 30a1d1849).
	modifierDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(modifierDir, ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modifierDir, ConfigDirName, "settings.json"),
		[]byte(`{"defaultTools":["+grep","-write"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	modifier := NewSettingsManagerFromFiles(modifierDir, agentDirForSettings(t), SettingsManagerCreateOptions{})
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: modifierDir, ModelRuntime: runtime, SettingsManager: modifier,
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read,bash,edit,grep,set_goal,get_goal" {
		t.Fatalf("tools = %q", names)
	}

	// An explicitly empty defaultTools means no tools, not the built-in
	// default: upstream reads it with nullish coalescing
	// (`configured ?? defaultActiveToolNames`), so an empty array is a value
	// and only an absent one falls through. The port checked for a non-empty
	// list, which made defaultTools: [] inexpressible.
	emptyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(emptyDir, ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyDir, ConfigDirName, "settings.json"),
		[]byte(`{"defaultTools":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := NewSettingsManagerFromFiles(emptyDir, agentDirForSettings(t), SettingsManagerCreateOptions{})
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: emptyDir, ModelRuntime: runtime, SettingsManager: empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := session.Session.GetActiveToolNames(); len(names) != 0 {
		t.Fatalf("tools = %v, want none for an explicitly empty defaultTools", names)
	}

	// The CLI projection feeds these options the way the binary does: a
	// --tools allowlist reaches the session through ToolSelection.
	cli := ParseArgs([]string{"--tools", "read,grep"})
	selection := cli.ToolSelection()
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings,
		Tools: selection.Tools, ExcludeTools: selection.ExcludeTools, NoTools: selection.NoTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read,grep" {
		t.Fatalf("tools = %q, want the CLI allowlist applied", names)
	}

	// A pattern entry selects every tool it matches (upstream #10343), so `re*` takes read and
	// leaves everything else out.
	cli = ParseArgs([]string{"--tools", "re*"})
	selection = cli.ToolSelection()
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings,
		Tools: selection.Tools, ExcludeTools: selection.ExcludeTools, NoTools: selection.NoTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); names != "read" {
		t.Fatalf("tools = %q, want the pattern to select read alone", names)
	}

	// A pattern entry removes every tool it matches, applied to the built-in defaults.
	cli = ParseArgs([]string{"--exclude-tools", "re*,*sh"})
	selection = cli.ToolSelection()
	session, err = CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime, SettingsManager: settings,
		Tools: selection.Tools, ExcludeTools: selection.ExcludeTools, NoTools: selection.NoTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(session.Session.GetActiveToolNames(), ","); strings.Contains(names, "read") || strings.Contains(names, "bash") {
		t.Fatalf("tools = %q, want the patterns to remove read and bash", names)
	}
}

// TestSetActiveToolsHonorsExplicitAllowlist follows core/agent-session.ts's
// _isAllowedTool filter when a loadout is applied.
func TestSetActiveToolsHonorsExplicitAllowlist(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	tool := agent.AgentTool{Name: "mcp__docs__search", Label: "docs/search", Description: "Docs search"}
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"read"}, ExtraTools: []agent.AgentTool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.Session.SetActiveToolsByName([]string{"read", "mcp__docs__search"})
	if got := strings.Join(session.Session.GetActiveToolNames(), ","); got != "read" {
		t.Fatalf("active tools after explicit set = %q, want allowlist read", got)
	}
}

// TestLateMCPToolsRespectToolsAllowlist follows core/agent-session.ts's
// _isAllowedTool and _refreshToolRegistry handling of tools registered after session start.
func TestLateMCPToolsRespectToolsAllowlist(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := agent.AgentTool{Name: "mcp__docs__search", Label: "docs/search", Description: "Docs search"}
	session.Session.AttachExtraTools([]agent.AgentTool{tool})
	if got := strings.Join(session.Session.GetActiveToolNames(), ","); got != "read" {
		t.Fatalf("active tools after MCP registration = %q, want explicit allowlist read", got)
	}
	if session.Session.GetToolDefinition(tool.Name) == nil {
		t.Fatal("MCP tool was not registered")
	}
}

// TestLateMCPToolsMatchToolsAllowlist follows the wildcard matcher applied
// during core/agent-session.ts's registry refresh.
func TestLateMCPToolsMatchToolsAllowlist(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"read", "mcp__docs__s*"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := agent.AgentTool{Name: "mcp__docs__search", Label: "docs/search", Description: "Docs search"}
	session.Session.AttachExtraTools([]agent.AgentTool{tool})
	if got := strings.Join(session.Session.GetActiveToolNames(), ","); got != "read,mcp__docs__search" {
		t.Fatalf("active tools after MCP registration = %q, want the matching tool enabled", got)
	}
}

// TestLateMCPToolsRespectNoToolsAll follows core/agent-session.ts's empty
// allowlist behavior for --no-tools.
func TestLateMCPToolsRespectNoToolsAll(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		NoTools: NoToolsAll,
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := agent.AgentTool{Name: "mcp__docs__search", Label: "docs/search", Description: "Docs search"}
	session.Session.AttachExtraTools([]agent.AgentTool{tool})
	if got := session.Session.GetActiveToolNames(); len(got) != 0 {
		t.Fatalf("active tools after --no-tools = %v, want none", got)
	}
}

// TestLateMCPToolsRespectTruthyNoToolsValue follows core/sdk.ts's truthy
// noTools handling for values other than the built-in-only mode.
func TestLateMCPToolsRespectTruthyNoToolsValue(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		NoTools: "other",
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := agent.AgentTool{Name: "mcp__docs__search", Label: "docs/search", Description: "Docs search"}
	session.Session.AttachExtraTools([]agent.AgentTool{tool})
	if got := session.Session.GetActiveToolNames(); len(got) != 0 {
		t.Fatalf("active tools after truthy noTools = %v, want none", got)
	}
}

// TestCreateAgentSessionPatternSelectionPreservesRegistryOrder follows the
// insertion-ordered registry in core/tools/index.ts and core/agent-session.ts.
func TestCreateAgentSessionPatternSelectionPreservesRegistryOrder(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	want := "read,bash,powershell,edit,write,grep,find,ls,set_goal,get_goal"
	for attempt := range 8 {
		session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
			Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
			Tools: []ToolName{"*"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(session.Session.GetActiveToolNames(), ","); got != want {
			t.Fatalf("attempt %d: tools = %q, want registry order %q", attempt, got, want)
		}
	}
}

// TestCreateAgentSessionPatternSelectionDeduplicatesOverlaps follows the
// loadout's insertion-ordered, set-based selection in core/agent-session.ts.
func TestCreateAgentSessionPatternSelectionDeduplicatesOverlaps(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Tools: []ToolName{"read", "r*", "grep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(session.Session.GetActiveToolNames(), ",")
	if got != "read,grep" {
		t.Fatalf("tools = %q, want read and grep once each", got)
	}
}
