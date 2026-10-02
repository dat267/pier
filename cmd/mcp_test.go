package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// TestSetupMCPServers covers the CLI's MCP plumbing: config errors are
// reported, a failing server yields no tools, and `enabled: false` is skipped.
func TestSetupMCPServers(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()

	// No mcp.json: nothing configured, no manager.
	manager, tools, errors := setupMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || tools != nil || len(errors) != 0 {
		t.Fatalf("unconfigured: manager=%v tools=%v errors=%v", manager, tools, errors)
	}

	// An invalid entry is reported and no server is registered.
	writeMCPConfig(t, agentDir, `{"mcpServers":{"bad name":{"command":"x"}}}`)
	manager, tools, errors = setupMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || tools != nil {
		t.Fatalf("invalid config: manager=%v tools=%v", manager, tools)
	}
	if len(errors) != 1 || !strings.Contains(errors[0], `invalid server name "bad name"`) {
		t.Fatalf("errors = %v", errors)
	}

	// A server whose command cannot run fails to connect; the manager keeps
	// the connection and reports it, and exposes no tools.
	writeMCPConfig(t, agentDir, `{"mcpServers":{"broken":{"command":"/nonexistent/pier-mcp-test"}}}`)
	manager, tools, errors = setupMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager == nil {
		t.Fatal("missing manager")
	}
	broken := manager
	defer func() { _ = broken.Close(context.Background()) }()
	if len(tools) != 0 {
		t.Fatalf("tools = %+v", tools)
	}
	if len(errors) != 1 || !strings.Contains(errors[0], "failed to connect") {
		t.Fatalf("errors = %v", errors)
	}

	// A disabled server is not connected.
	writeMCPConfig(t, agentDir, `{"mcpServers":{"off":{"command":"/nonexistent/pier-mcp-test","enabled":false}}}`)
	manager, tools, errors = setupMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || tools != nil || len(errors) != 0 {
		t.Fatalf("disabled: manager=%v tools=%v errors=%v", manager, tools, errors)
	}
}

// TestSetupMCPServersProjectScope covers the trusted-project mcp.json.
func TestSetupMCPServersProjectScope(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, coding.ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := `{"mcpServers":{"proj":{"command":"/nonexistent/pier-mcp-test"}}}`
	if err := os.WriteFile(filepath.Join(cwd, coding.ConfigDirName, "mcp.json"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	// Untrusted: the project file is ignored.
	manager, _, errors := setupMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || len(errors) != 0 {
		t.Fatalf("untrusted: manager=%v errors=%v", manager, errors)
	}
	// Trusted: it is loaded (and the server fails to start, which is reported).
	manager, _, errors = setupMCPServers(context.Background(), nil, agentDir, cwd, true)
	if manager == nil {
		t.Fatal("trusted project config ignored")
	}
	project := manager
	defer func() { _ = project.Close(context.Background()) }()
	if len(manager.Connections()) != 1 || manager.Connections()[0].Name() != "proj" {
		t.Fatalf("connections = %+v", manager.Connections())
	}
	if len(errors) != 1 || !strings.Contains(errors[0], "failed to connect") {
		t.Fatalf("errors = %v", errors)
	}
}

func writeMCPConfig(t *testing.T, agentDir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
