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
			// D286: the port adds one rule upstream lacks. The golden stays the
			// upstream output, so drop the line here and assert its presence
			// separately: a regenerated golden must not hide a lost rule.
			hadShellWriteRule := strings.Contains(sections["rules"], shellWriteRulePrefix)
			if rules, ok := sections["rules"]; ok {
				sections["rules"] = stripShellWriteRules(rules)
			}
			if hadShellWriteRule != shellWriteRuleCases[name] {
				t.Errorf("shell-write rule present = %v, want %v", hadShellWriteRule, shellWriteRuleCases[name])
			}
			if !reflect.DeepEqual(map[string]string(sections), want.Sections) {
				t.Errorf("sections = %#v, want %#v", sections, want.Sections)
			}
		})
	}
}

// shellWriteRuleCases lists the cases that carry the D286 rule: a shell tool and
// edit or write selected, including the omitted-list defaults.
var shellWriteRuleCases = map[string]bool{
	"defaults":            true,
	"cwd-windows":         true,
	"custom-section":      true,
	"snippets-guidelines": true,
}

// shellWriteRulePrefix identifies the D286 rule line.
const shellWriteRulePrefix = "- Do not write or edit files through "

// stripShellWriteRules removes the D286 rule line from a rules section.
func stripShellWriteRules(rules string) string {
	lines := strings.Split(rules, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, shellWriteRulePrefix) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
