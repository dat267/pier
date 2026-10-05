package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// TestSetupMCPServers covers the CLI's MCP plumbing: config errors are
// reported, a failing server yields no tools, and `enabled: false` is skipped.
func TestSetupMCPServers(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()

	// No mcp.json: nothing configured, no manager.
	manager, errors := startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || len(errors) != 0 {
		t.Fatalf("unconfigured: manager=%v errors=%v", manager, errors)
	}

	// An invalid entry is reported and no server is registered.
	writeMCPConfig(t, agentDir, `{"mcpServers":{"bad name":{"command":"x"}}}`)
	manager, errors = startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil {
		t.Fatalf("invalid config: manager=%v", manager)
	}
	if len(errors) != 1 || !strings.Contains(errors[0], `invalid server name "bad name"`) {
		t.Fatalf("errors = %v", errors)
	}

	// A server whose command cannot run fails to connect; the manager keeps
	// the connection and reports it, and exposes no tools. The connect runs in
	// the background, so the failure surfaces on the connection, not in the
	// boot-time errors (which are config errors only).
	writeMCPConfig(t, agentDir, `{"mcpServers":{"broken":{"command":"/nonexistent/pier-mcp-test"}}}`)
	manager, errors = startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager == nil {
		t.Fatal("missing manager")
	}
	broken := manager
	defer func() { _ = broken.Close(context.Background()) }()
	if len(errors) != 0 {
		t.Fatalf("boot errors = %v", errors)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(broken.ConnectionErrors()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if connectionErrors := broken.ConnectionErrors(); len(connectionErrors) != 1 || !strings.Contains(connectionErrors[0], "failed to connect") {
		t.Fatalf("connection errors = %v", connectionErrors)
	}
	if len(broken.DirectTools()) != 0 {
		t.Fatalf("tools = %+v", broken.DirectTools())
	}

	// A disabled server is not connected.
	writeMCPConfig(t, agentDir, `{"mcpServers":{"off":{"command":"/nonexistent/pier-mcp-test","enabled":false}}}`)
	manager, errors = startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || len(errors) != 0 {
		t.Fatalf("disabled: manager=%v errors=%v", manager, errors)
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
	manager, errors := startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil || len(errors) != 0 {
		t.Fatalf("untrusted: manager=%v errors=%v", manager, errors)
	}
	// Trusted: it is loaded (and the server fails to start, which is reported
	// on the connection once the background connect settles).
	manager, errors = startMCPServers(context.Background(), nil, agentDir, cwd, true)
	if manager == nil {
		t.Fatal("trusted project config ignored")
	}
	project := manager
	defer func() { _ = project.Close(context.Background()) }()
	if len(manager.Connections()) != 1 || manager.Connections()[0].Name() != "proj" {
		t.Fatalf("connections = %+v", manager.Connections())
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(manager.ConnectionErrors()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if connectionErrors := manager.ConnectionErrors(); len(connectionErrors) != 1 || !strings.Contains(connectionErrors[0], "failed to connect") {
		t.Fatalf("connection errors = %v", connectionErrors)
	}
}

func writeMCPConfig(t *testing.T, agentDir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPrintMCPConnectingStatus pins the startup status line: the connect runs
// in the background while the boot continues, so the CLI names what it is
// connecting to when it starts it (singular quoted name, plural count + list,
// none silent).
func TestPrintMCPConnectingStatus(t *testing.T) {
	var buf bytes.Buffer
	printMCPConnectingStatus(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("no servers: wrote %q", buf.String())
	}

	buf.Reset()
	printMCPConnectingStatus(&buf, []string{"deepwiki"})
	if got := buf.String(); !strings.Contains(got, `Connecting to MCP server "deepwiki"`) {
		t.Fatalf("single: got %q", got)
	}

	buf.Reset()
	printMCPConnectingStatus(&buf, []string{"a", "b"})
	if got := buf.String(); !strings.Contains(got, "Connecting to 2 MCP servers: a, b") {
		t.Fatalf("plural: got %q", got)
	}
}

// TestSetupMCPServersPrintsConnectingStatus covers the wiring: an enabled
// server prints the line; a disabled one does not.
func TestSetupMCPServersPrintsConnectingStatus(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()

	var buf bytes.Buffer
	restored := false
	mcpStatusWriter = &buf
	defer func() {
		if !restored {
			mcpStatusWriter = os.Stderr
		}
	}()

	writeMCPConfig(t, agentDir, `{"mcpServers":{"deepwiki":{"url":"https://mcp.deepwiki.invalid/mcp"}}}`)
	manager, _ := startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil {
		_ = manager.Close(context.Background())
	}
	if !strings.Contains(buf.String(), `Connecting to MCP server "deepwiki"`) {
		t.Fatalf("enabled server: got %q", buf.String())
	}

	restored = true
	mcpStatusWriter = os.Stderr
	buf.Reset()
	writeMCPConfig(t, agentDir, `{"mcpServers":{"off":{"command":"/nonexistent/pier-mcp-test","enabled":false}}}`)
	manager, _ = startMCPServers(context.Background(), nil, agentDir, cwd, false)
	if manager != nil {
		_ = manager.Close(context.Background())
	}
	if buf.Len() != 0 {
		t.Fatalf("disabled server: wrote %q", buf.String())
	}
}
