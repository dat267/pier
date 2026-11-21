package ai

import "testing"

// The developer role goes only to OpenAI's own API by default: many
// OpenAI-compatible hosts reject it (Groq's Qwen returned "400 Unexpected
// message role"), and every host accepts system. `compat.supportsDeveloperRole`
// still overrides the detection
// (openai-completions.ts detectCompat, v1.6.0, #10741).
func TestOpenAICompletionsDeveloperRoleDefaultsToOpenAI(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		baseURL  string
		modelID  string
		want     bool
	}{
		{"openai host", "openai", "https://api.openai.com/v1", "gpt-5", true},
		{"openai provider", "openai", "", "gpt-5", true},
		{"azure", "azure", "https://example.openai.azure.com", "gpt-5", false},
		{"groq", "groq", "https://api.groq.com/openai/v1", "qwen/qwen3-32b", false},
		{"openrouter", "openrouter", "https://openrouter.ai/api/v1", "openai/gpt-5", false},
		{"custom host", "local", "http://localhost:1234/v1", "local-model", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := GetOpenAICompletionsCompat(&Model{
				ID: tc.modelID, Provider: tc.provider, API: APIOpenAICompletions, BaseURL: tc.baseURL,
			})
			if resolved.SupportsDeveloperRole != tc.want {
				t.Fatalf("supportsDeveloperRole = %v, want %v", resolved.SupportsDeveloperRole, tc.want)
			}
		})
	}
	// An explicit compat value wins over the detection.
	on := true
	resolved := GetOpenAICompletionsCompat(&Model{
		ID: "gpt-5", Provider: "azure", API: APIOpenAICompletions,
		Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{SupportsDeveloperRole: &on}},
	})
	if !resolved.SupportsDeveloperRole {
		t.Fatal("an explicit supportsDeveloperRole was ignored")
	}
}
