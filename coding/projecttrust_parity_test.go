package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// pi v1.4.0 core/trust-manager.ts includes mcp.json in trust-requiring
// resources. core/project-trust.ts must not short-circuit refusal for such a
// project. Config loading is safe here: the fixture command is never executed.
// Regenerate from repository root:
// node coding/testdata/project-trust/generate.mjs ./pi > coding/testdata/project-trust/golden.json
func TestMCPOnlyProjectRequiresTrust(t *testing.T) {
	raw, err := os.ReadFile("testdata/project-trust/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []struct {
		Name      string   `json:"name"`
		Resources []string `json:"resources"`
		Policy    string   `json:"policy"`
		Stored    *bool    `json:"stored"`
		Override  *bool    `json:"override"`
		Selection string   `json:"selection"`
		Expected  struct {
			HasResources bool `json:"hasResources"`
			Trusted      bool `json:"trusted"`
			Prompts      int  `json:"prompts"`
			Servers      int  `json:"servers"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 {
		t.Fatal("empty project trust fixture")
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			root := t.TempDir()
			project, agentDir := filepath.Join(root, "project"), filepath.Join(root, "agent")
			if err := os.MkdirAll(filepath.Join(project, ConfigDirName), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, resource := range test.Resources {
				content := `{}`
				if resource == "mcp.json" {
					content = `{"mcpServers":{"fixture":{"command":"not-executed"}}}`
				}
				if err := os.WriteFile(filepath.Join(project, ConfigDirName, resource), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := NewProjectTrustStore(agentDir)
			if test.Stored != nil {
				if err := store.Set(project, test.Stored); err != nil {
					t.Fatal(err)
				}
			}
			prompts := 0
			trusted, err := ResolveProjectTrusted(ResolveProjectTrustedOptions{
				Cwd: project, TrustStore: store, DefaultProjectTrust: test.Policy, TrustOverride: test.Override,
				ProjectTrustContext: ProjectTrustContext{
					HasUI:  test.Selection != "",
					Select: func(string, []string) (string, error) { prompts++; return test.Selection, nil },
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if resources := HasTrustRequiringProjectResources(project); resources != test.Expected.HasResources {
				t.Errorf("hasResources = %v, want %v", resources, test.Expected.HasResources)
			}
			if trusted != test.Expected.Trusted || prompts != test.Expected.Prompts {
				t.Errorf("trusted=%v prompts=%d, want trusted=%v prompts=%d", trusted, prompts, test.Expected.Trusted, test.Expected.Prompts)
			}
			config := LoadMcpConfig(McpConfigLoadOptions{Cwd: project, AgentDir: agentDir, ProjectTrusted: trusted})
			if len(config.Errors) > 0 {
				t.Fatal(config.Errors)
			}
			if len(config.Servers) != test.Expected.Servers {
				t.Errorf("admitted servers = %d, want %d", len(config.Servers), test.Expected.Servers)
			}
		})
	}
}
