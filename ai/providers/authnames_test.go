package providers

import "testing"

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
