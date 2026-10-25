package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type mcpConfigFixture struct {
	Validate []struct {
		Name              string          `json:"name"`
		ServerName        string          `json:"serverName"`
		Raw               json.RawMessage `json:"raw"`
		ToolExposureOrder []string        `json:"toolExposureOrder"`
		Error             *string         `json:"error"`
		Config            json.RawMessage `json:"config"`
	} `json:"validate"`
	Load []struct {
		Name    string          `json:"name"`
		Config  json.RawMessage `json:"config"`
		Servers []string        `json:"servers"`
		Errors  []string        `json:"errors"`
	} `json:"load"`
}

// loadMCPConfigFixture reads the source-generated pinned fixtures.
// Regenerate with
// node coding/testdata/mcp-config/generate.mjs ./pi > coding/testdata/mcp-config/golden.json
func loadMCPConfigFixture(t *testing.T) mcpConfigFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/mcp-config/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mcpConfigFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Validate) == 0 || len(fixture.Load) == 0 {
		t.Fatal("fixture is empty")
	}
	return fixture
}

// TestMcpServerConfigValidationMatchesUpstream pins validateMcpServerConfig
// against the pinned source, including a non-string `type`, WHATWG URL
// whitespace, invalid-name wording and the toolExposure error order.
func TestMcpServerConfigValidationMatchesUpstream(t *testing.T) {
	fixture := loadMCPConfigFixture(t)
	for _, test := range fixture.Validate {
		t.Run(test.Name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal(test.Raw, &value); err != nil {
				t.Fatal(err)
			}
			config, err := ValidateMcpServerConfigOrdered(test.ServerName, value, test.ToolExposureOrder)
			if test.Error != nil {
				if err == nil {
					t.Fatalf("want error %q, got config %+v", *test.Error, config)
				}
				if err.Error() != *test.Error {
					t.Errorf("error = %q, want %q", err.Error(), *test.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if config == nil {
				t.Fatal("accepted entry returned no config")
			}
		})
	}
}

// TestMcpConfigLoadMatchesUpstream pins loadMcpConfig: which entries load and
// the exact error list, with the agent directory folded to <path>.
func TestMcpConfigLoadMatchesUpstream(t *testing.T) {
	fixture := loadMCPConfigFixture(t)
	for _, test := range fixture.Load {
		t.Run(test.Name, func(t *testing.T) {
			agentDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), test.Config, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir, Cwd: agentDir})
			servers := make([]string, 0, len(loaded.Servers))
			for _, server := range loaded.Servers {
				servers = append(servers, server.Name)
			}
			if !reflect.DeepEqual(servers, test.Servers) {
				t.Errorf("servers = %v, want %v", servers, test.Servers)
			}
			errors := make([]string, 0, len(loaded.Errors))
			for _, entry := range loaded.Errors {
				errors = append(errors, strings.ReplaceAll(entry, agentDir, "<path>"))
			}
			if !reflect.DeepEqual(errors, test.Errors) {
				t.Errorf("errors = %q, want %q", errors, test.Errors)
			}
		})
	}
}
