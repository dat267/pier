package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) core/package-manager.ts resolves project
// resources inside `if (projectTrusted)`, and prompts are one of them
// (`addResources("prompts", collectAutoPromptEntries(projectDirs.prompts), ...)`).
// `prompts` is also in TRUST_REQUIRING_PROJECT_CONFIG_RESOURCES, which is what
// raises the question. A487: the port gated skills, themes, MCP config and the
// system/append prompt files but loaded the project's prompts in both states.
func promptTemplateNames(templates []PromptTemplate) []string {
	names := make([]string, 0, len(templates))
	for _, template := range templates {
		names = append(names, template.Name)
	}
	return names
}

func writePromptTemplate(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# " + name + "\n\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectPromptsRequireTrust(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	writePromptTemplate(t, filepath.Join(cwd, ConfigDirName, "prompts"), "project-command")
	writePromptTemplate(t, filepath.Join(agentDir, "prompts"), "user-command")

	options := LoadPromptTemplatesOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true}

	options.ProjectTrusted = false
	untrusted := promptTemplateNames(LoadPromptTemplatesWithDiagnostics(options).Templates)
	if strings.Contains(strings.Join(untrusted, ","), "project-command") {
		t.Errorf("untrusted project prompts loaded: %v, want only the user's", untrusted)
	}
	if !strings.Contains(strings.Join(untrusted, ","), "user-command") {
		t.Errorf("the user's own prompts stopped loading when untrusted: %v", untrusted)
	}

	options.ProjectTrusted = true
	trusted := promptTemplateNames(LoadPromptTemplatesWithDiagnostics(options).Templates)
	if !strings.Contains(strings.Join(trusted, ","), "project-command") {
		t.Errorf("a trusted project's prompts did not load: %v", trusted)
	}
}
