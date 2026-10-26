package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Reference: pi v1.4.0 (4a080252), interactive-mode.ts
// getLoginProviderOptions and oauth-selector.ts formatAuthSelectorProviderType.
func TestAuthLoginSelectorLabelsNonSubscriptionAccounts(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	providers := wiring.GetLoginProviderOptions("")
	for _, tc := range []struct{ query, want string }{
		{"OpenRouter", "OpenRouter [account]"},
		{"Anthropic", "Anthropic [subscription]"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			selector := NewOAuthSelectorComponent("login", providers, nil, nil, tc.query)
			text := tui.StripTerminalSequences(strings.Join(selector.Render(120), "\n"))
			if !strings.Contains(text, tc.want) {
				t.Fatalf("missing %q in login selector:\n%s", tc.want, text)
			}
		})
	}
}

// The same flag must survive the stored-credential /logout path, which has no
// Method object to infer it from. Reference: the pinned getLogoutProviderOptions.
func TestAuthLogoutSelectorLabelsNonSubscriptionAccounts(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	store := ai.NewInMemoryCredentialStore()
	for _, provider := range []string{"openrouter", "anthropic", "openai"} {
		_, err := store.Modify(provider, func(*ai.Credential) (*ai.Credential, error) {
			if provider == "openai" {
				return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "synthetic-key"}}, nil
			}
			return &ai.Credential{Type: ai.CredentialOAuth, OAuth: &ai.OAuthCredential{
				OAuthCredentials: ai.OAuthCredentials{Access: "synthetic-access", Refresh: "synthetic-refresh", Expires: 4102444800000},
			}}, nil
		}, context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	refresh := false
	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{
		Credentials: store, DisableModelsJSON: true, RefreshOnCreate: &refresh,
	})
	if err != nil {
		t.Fatal(err)
	}
	wiring := &AuthWiring{Session: &authTestSession{runtime: runtime}}
	providers, err := wiring.GetLogoutProviderOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ query, want string }{
		{"OpenRouter", "OpenRouter [account]"},
		{"Anthropic", "Anthropic [subscription]"},
		{"OpenAI", "OpenAI [API key]"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			selector := NewOAuthSelectorComponent("logout", providers, nil, nil, tc.query)
			text := tui.StripTerminalSequences(strings.Join(selector.Render(120), "\n"))
			if !strings.Contains(text, tc.want) {
				t.Fatalf("missing %q in logout selector:\n%s", tc.want, text)
			}
		})
	}
}
