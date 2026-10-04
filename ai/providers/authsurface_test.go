package providers

import "testing"

// TestBuiltinProviderOAuthSurface pins which built-in providers advertise
// OAuth, and the flow name, so a provider cannot silently lose (or gain) one
// (upstream builtinProviders' auth blocks).
func TestBuiltinProviderOAuthSurface(t *testing.T) {
	byID := map[string]bool{}
	names := map[string]string{}
	for _, provider := range BuiltinProviders() {
		byID[provider.ID] = true
		if provider.Auth.OAuth != nil {
			names[provider.ID] = provider.Auth.OAuth.Name
		}
	}
	expected := map[string]string{
		"anthropic":      "Anthropic (Claude Pro/Max)",
		"openai":         "OpenAI (ChatGPT subscription)",
		"openai-codex":   "OpenAI (ChatGPT Plus/Pro)",
		"github-copilot": "GitHub Copilot",
		"openrouter":     "OpenRouter OAuth",
		"xai":            "xAI (Grok/X subscription)",
		"kimi-coding":    "Kimi Code (subscription)",
		"meta":           "Meta (Muse subscription)",
		"radius":         "", // the name comes from the gateway config
	}
	for id, want := range expected {
		if !byID[id] {
			t.Errorf("missing provider %q", id)
			continue
		}
		got, ok := names[id]
		if !ok {
			t.Errorf("%s has no oauth auth", id)
			continue
		}
		if want != "" && got != want {
			t.Errorf("%s oauth name = %q, want %q", id, got, want)
		}
	}
	// Every provider with an OAuth auth is in the expected set.
	for id := range names {
		if _, ok := expected[id]; !ok {
			t.Errorf("%s unexpectedly advertises oauth %q", id, names[id])
		}
	}
}
