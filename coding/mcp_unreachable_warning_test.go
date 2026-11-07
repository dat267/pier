package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// D185: pier has no codemode or tool_search tool, so `codemode` and `deferred`
// MCP tools register but can never be called.
//
// Reference: pi v1.5.0 extensions/mcp/index.ts `ensureDiscoveryActive` warns
// from the config, before any server connects, when an enabled server's
// `configuredExposures` (its exposure, default codemode, plus its
// toolExposure values) include `codemode` or `deferred` and no tool reaches
// them. Pier's warning keeps that condition and names the servers and the fix.
func TestMcpUnreachableToolsWarningFollowsConfiguredExposures(t *testing.T) {
	load := func(t *testing.T, body string) LoadedMcpConfig {
		t.Helper()
		agentDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		config := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir, Cwd: t.TempDir()})
		if len(config.Errors) != 0 {
			t.Fatalf("config errors: %v", config.Errors)
		}
		return config
	}
	cases := []struct {
		name string
		body string
		want string // a fragment of the warning, or "" for none
	}{
		{"default exposure is codemode", `{"mcpServers":{"gopls":{"command":"gopls"}}}`, `MCP server "gopls" has tools`},
		{"explicit codemode", `{"mcpServers":{"a":{"command":"x","exposure":"codemode"}}}`, `MCP server "a" has`},
		{"deferred", `{"mcpServers":{"a":{"command":"x","exposure":"deferred"}}}`, `MCP server "a" has`},
		{"direct", `{"mcpServers":{"a":{"command":"x","exposure":"direct"}}}`, ""},
		{"hidden is intentional", `{"mcpServers":{"a":{"command":"x","exposure":"hidden"}}}`, ""},
		{"direct with one hidden tool", `{"mcpServers":{"a":{"command":"x","exposure":"direct","toolExposure":{"t":"hidden"}}}}`, ""},
		{"direct with one codemode tool", `{"mcpServers":{"a":{"command":"x","exposure":"direct","toolExposure":{"t":"codemode"}}}}`, `MCP server "a" has`},
		{"disabled server", `{"mcpServers":{"a":{"command":"x","enabled":false}}}`, ""},
		{"no servers", `{"mcpServers":{}}`, ""},
		{"servers listed in config order", `{"mcpServers":{"b":{"command":"x"},"ok":{"command":"x","exposure":"direct"},"a":{"command":"x","exposure":"deferred"}}}`, `MCP servers "b", "a" have tools`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := McpUnreachableToolsWarning(load(t, tc.body))
			if tc.want == "" {
				if got != "" {
					t.Fatalf("warning = %q, want none", got)
				}
				return
			}
			if !strings.HasPrefix(got, tc.want) || !strings.Contains(got, `"exposure": "direct"`) {
				t.Fatalf("warning = %q, want prefix %q and the direct-exposure fix", got, tc.want)
			}
		})
	}
}
