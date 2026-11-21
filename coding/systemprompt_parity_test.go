package coding

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type systemPromptCase struct {
	options BuildSystemPromptOptions
}

// TestSystemPromptSectionsMatchUpstream pins buildSystemPromptSections section
// for section. An explicit empty tool list must stay empty (no default-tool
// rules and no skills block); only an omitted list gets the defaults.
// Regenerate with
// node coding/testdata/system-prompt/generate.mjs ./pi > coding/testdata/system-prompt/golden.json
func TestSystemPromptSectionsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/system-prompt/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Sections map[string]string `json:"sections"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	skill := Skill{Name: "deploy", Description: "Deploy <prod> & more", FilePath: "/work/.pi/skills/deploy/SKILL.md"}
	cases := map[string]systemPromptCase{
		"defaults":                {BuildSystemPromptOptions{Cwd: "/work"}},
		"empty-tools":             {BuildSystemPromptOptions{SelectedTools: []string{}, Cwd: "/work"}},
		"empty-tools-with-skills": {BuildSystemPromptOptions{SelectedTools: []string{}, Skills: []Skill{skill}, Cwd: "/work"}},
		"read-only-skills":        {BuildSystemPromptOptions{SelectedTools: []string{"read"}, Skills: []Skill{skill}, Cwd: "/work"}},
		"bash-grep": {BuildSystemPromptOptions{
			SelectedTools: []string{"bash", "grep"}, Cwd: "/work",
			ToolSnippets:   map[string]string{"bash": "Run commands", "grep": "Search contents"},
			ToolGuidelines: map[string][]string{"bash": {"Use bash wisely"}, "grep": {"Be exact"}},
		}},
		"powershell-only": {BuildSystemPromptOptions{
			SelectedTools: []string{"powershell"}, Cwd: "/work",
			ToolSnippets:   map[string]string{"powershell": "Run PowerShell"},
			ToolGuidelines: map[string][]string{"powershell": {"Use PS"}},
		}},
		"prompt-guidelines": {BuildSystemPromptOptions{
			SelectedTools: []string{"read"}, PromptGuidelines: []string{"  Be concise ", "Be concise", "Extra rule"}, Cwd: "/work",
		}},
		"custom-append-context": {BuildSystemPromptOptions{
			CustomPrompt: "Custom system prompt.", AppendSystemPrompt: "Appended text.",
			ContextFiles: []ContextFile{{Path: "/work/AGENTS.md", Content: "project rules"}}, Cwd: "/work",
		}},
		"snippets-guidelines": {BuildSystemPromptOptions{
			SelectedTools: []string{"read", "bash", "edit"}, Cwd: "/work",
			ToolSnippets:   map[string]string{"read": "Read files", "bash": "Run commands", "edit": "Edit files"},
			ToolGuidelines: map[string][]string{"read": {"Read first"}, "bash": {"Run second"}, "edit": {"Edit third"}},
		}},
		"cwd-windows":    {BuildSystemPromptOptions{Cwd: `C:\work\proj`}},
		"custom-section": {BuildSystemPromptOptions{Cwd: "/work", Sections: map[string]string{"extra": "extra text"}}},
		"hidden-tools": {BuildSystemPromptOptions{
			SelectedTools: []string{"read", "bash", "edit"}, HiddenTools: []string{"bash"}, Cwd: "/work",
			ToolSnippets:   map[string]string{"read": "Read files", "bash": "Run commands", "edit": "Edit files"},
			ToolGuidelines: map[string][]string{"read": {"Read first"}, "bash": {"Run second"}, "edit": {"Edit third"}},
		}},
		"hidden-reader-with-skills": {BuildSystemPromptOptions{
			SelectedTools: []string{"read", "bash"}, HiddenTools: []string{"read", "bash"}, Cwd: "/work",
			Skills: []Skill{{Name: "deploy", Description: "Deploy <prod> & more", FilePath: "/work/.pi/skills/deploy/SKILL.md"}},
		}},
	}
	if len(fixture) != len(cases) {
		t.Fatalf("fixture cases = %d, table = %d", len(fixture), len(cases))
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			want, ok := fixture[name]
			if !ok {
				t.Fatalf("no fixture for %q", name)
			}
			sections, err := BuildSystemPromptSections(test.options)
			if err != nil {
				t.Fatal(err)
			}
			// D176: the port omits upstream's docs section.
			delete(sections, "docs")
			// D499: the only difference left is the harness name the preamble
			// announces, so check this port's sentence and then substitute the
			// reference's name for the comparison.
			// A custom prompt replaces the preamble, so only the default one
			// carries the name.
			if test.options.CustomPrompt == "" {
				preamble, ok := sections["preamble"]
				if !ok || !strings.HasPrefix(preamble, "You are a coding assistant running inside "+AgentName+",") {
					t.Errorf("preamble = %q, want the reference sentence with %q", preamble, AgentName)
				}
				sections["preamble"] = strings.Replace(preamble, "running inside "+AgentName, "running inside pi", 1)
			}
			if !reflect.DeepEqual(map[string]string(sections), want.Sections) {
				t.Errorf("sections = %#v, want %#v", sections, want.Sections)
			}
		})
	}
}
