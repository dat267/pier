package coding

// D286: pier asks the model not to modify files through the shell. Upstream
// pi v1.4.0 (4a080252) tool contributions never say this; jcode's system prompt
// carries the same instruction ("Edit files with `edit`, `replace`,
// `apply_patch`, or `write`, not sed, perl, or Python scripts in bash",
// crates/jcode-base/src/prompt/system_prompt.md).

import (
	"strings"
	"testing"
)

func shellWriteRulePrompt(t *testing.T, tools ...string) string {
	t.Helper()
	prompt, err := BuildSystemPrompt(BuildSystemPromptOptions{
		SelectedTools:  tools,
		ToolSnippets:   builtinToolPromptSnippets,
		ToolGuidelines: builtinToolPromptGuidelines,
		Cwd:            "/work",
	})
	if err != nil {
		t.Fatalf("BuildSystemPrompt: %v", err)
	}
	return prompt
}

func TestSystemPromptForbidsShellFileWrites(t *testing.T) {
	prompt := shellWriteRulePrompt(t, "read", "bash", "edit", "write")
	want := "- Do not write or edit files through bash (redirection, heredocs, sed, perl, python); use the edit tool for changes to an existing file and the write tool for a new file or a full rewrite."
	if !strings.Contains(prompt, want) {
		t.Fatalf("prompt is missing the shell-write rule %q:\n%s", want, prompt)
	}
}

func TestSystemPromptShellWriteRuleNamesTheAvailableFileTool(t *testing.T) {
	for _, test := range []struct {
		tools []string
		want  string
	}{
		{[]string{"bash", "edit"}, "use the edit tool for every file change."},
		{[]string{"bash", "write"}, "use the write tool for every file change."},
		{[]string{"write", "powershell"}, "Do not write or edit files through PowerShell (redirection, heredocs, sed, perl, python); use the write tool for every file change."},
	} {
		prompt := shellWriteRulePrompt(t, test.tools...)
		if !strings.Contains(prompt, test.want) {
			t.Fatalf("tools %v: prompt is missing %q:\n%s", test.tools, test.want, prompt)
		}
	}
}

// Without edit or write the rule would name a tool the model does not have.
func TestSystemPromptShellWriteRuleNeedsAFileTool(t *testing.T) {
	for _, tools := range [][]string{{"read", "bash"}, {"read", "bash", "grep", "find", "ls"}} {
		prompt := shellWriteRulePrompt(t, tools...)
		if strings.Contains(prompt, "Do not write or edit files through") {
			t.Fatalf("tools %v: rule must not appear without edit/write:\n%s", tools, prompt)
		}
	}
}
