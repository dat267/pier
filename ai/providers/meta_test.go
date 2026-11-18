package providers

import (
	"testing"
)

// The Meta provider is registered like every other built-in (upstream
// metaProvider in builtinProviders()).
func TestMetaProviderIsRegistered(t *testing.T) {
	provider := MetaProvider()
	if provider.ID != "meta" || provider.Name != "Meta" {
		t.Fatalf("provider = %s/%s", provider.ID, provider.Name)
	}
	if provider.BaseURL != "https://api.meta.ai/v1" {
		t.Fatalf("baseUrl = %q", provider.BaseURL)
	}
	if provider.Auth.APIKey == nil {
		t.Fatal("meta has no api-key auth")
	}
	if provider.Auth.OAuth == nil || provider.Auth.OAuth.Name != "Meta (Muse subscription)" {
		t.Fatalf("oauth = %+v", provider.Auth.OAuth)
	}
	if len(provider.GetModels()) == 0 {
		t.Fatal("the meta provider has no models")
	}

	registered := false
	for _, id := range BuiltinProviderIDs {
		if id == "meta" {
			registered = true
		}
	}
	if !registered {
		t.Fatal("meta is missing from BuiltinProviderIDs")
	}
	found := false
	for _, builtin := range BuiltinProviders() {
		if builtin.ID == "meta" {
			found = true
		}
	}
	if !found {
		t.Fatal("BuiltinProviders does not construct meta")
	}
}

// Reference: pi v1.5.2 (b4afdddf) packages/ai/src/providers, where each factory
// names its env API-key auth explicitly. The port's EnvAPIKeyProvider defaults
// the name to "<provider name> API key", which differs from the reference for
// these five; the name is the login prompt ("Enter <name>") and the auth list
// label.
func TestProviderAuthNamesMatchTheReference(t *testing.T) {
	cases := map[string]string{
		"google":        "Gemini API key",      // google.ts envApiKeyAuth
		"kimi-coding":   "Kimi API key",        // kimi-coding.ts envApiKeyAuth
		"moonshotai-cn": "Moonshot AI API key", // moonshotai-cn.ts envApiKeyAuth
		"opencode":      "OpenCode API key",    // opencode.ts envApiKeyAuth
		"opencode-go":   "OpenCode API key",    // opencode-go.ts envApiKeyAuth
	}
	for id, want := range cases {
		provider := builtinProvider(id)
		if provider == nil {
			t.Fatalf("%s: no provider", id)
		}
		if provider.Auth.APIKey == nil {
			t.Fatalf("%s: no api-key auth", id)
		}
		if got := provider.Auth.APIKey.Name; got != want {
			t.Errorf("%s auth name = %q, want %q", id, got, want)
		}
	}
}

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
