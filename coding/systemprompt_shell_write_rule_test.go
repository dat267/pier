package coding

// D286 (retired): the rules are the reference's, with no port-only instruction. The port
// used to add "Change files with the edit and write tools, never the shell."
// (D286); it did not change model behavior and it broke byte parity with
// `core/system-prompt.ts` `buildRules`, so the prompt now carries only upstream's
// lines. These tests keep the rule from creeping back.

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

// TestSystemPromptHasNoShellWriteRule pins the alignment: with any tool set, the
// rules are the reference's lines, and no port-only instruction about the shell
// appears.
func TestSystemPromptHasNoShellWriteRule(t *testing.T) {
	for _, tools := range [][]string{
		{"read", "bash", "edit", "write"},
		{"bash", "edit"},
		{"bash", "write"},
		{"write", "powershell"},
		{"read", "bash"},
		{"read", "bash", "grep", "find", "ls"},
	} {
		prompt := shellWriteRulePrompt(t, tools...)
		if strings.Contains(prompt, "never the shell") || strings.Contains(prompt, "Change files with") {
			t.Fatalf("tools %v: the prompt carries a port-only file-write rule:\n%s", tools, prompt)
		}
	}
}

// TestSystemPromptPreambleMatchesTheReference pins the preamble: the reference's
// sentence, verbatim, including the harness name it announces.
func TestSystemPromptPreambleMatchesTheReference(t *testing.T) {
	prompt := shellWriteRulePrompt(t, "read", "bash")
	want := "You are a coding assistant running inside pi, a coding agent harness. Help users inspect files, run commands, and modify code."
	if !strings.Contains(prompt, want) {
		t.Fatalf("preamble is not the reference's sentence:\n%s", prompt)
	}
}
