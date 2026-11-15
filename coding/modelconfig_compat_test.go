package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi v1.5.2 b4afdddf packages/ai/src/providers/compat-schema.ts defines
// `thinkingTokenBudgetField` as a union of "thinking_token_budget",
// "thinking_budget" and "thinking_budget_tokens". Every compat field the port
// decodes must also be validated, or a value the reference refuses loads here.
func TestLoadModelConfigValidatesThinkingTokenBudgetField(t *testing.T) {
	load := func(t *testing.T, compat string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "models.json")
		content := `{"providers": {"custom": {"api": "openai-completions", "models": [{"id": "m", "compat": ` + compat + `}]}}}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return LoadModelConfig(path).GetError()
	}
	for _, invalid := range []string{`{"thinkingTokenBudgetField": 42}`, `{"thinkingTokenBudgetField": "thinking_budget_tokens_v2"}`} {
		if message := load(t, invalid); !strings.Contains(message, "providers.custom.models.0.compat.thinkingTokenBudgetField") {
			t.Fatalf("%s: error = %q, want a thinkingTokenBudgetField path", invalid, message)
		}
	}
	for _, valid := range []string{
		`{"thinkingTokenBudgetField": "thinking_token_budget"}`,
		`{"thinkingTokenBudgetField": "thinking_budget"}`,
		`{"thinkingTokenBudgetField": "thinking_budget_tokens"}`,
	} {
		if message := load(t, valid); message != "" {
			t.Fatalf("%s: unexpected error %q", valid, message)
		}
	}
}
