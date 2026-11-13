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
		{"OpenRouter", "OpenRouter \u2726 [account]"},
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
				Access: "synthetic-access", Refresh: "synthetic-refresh", Expires: 4102444800000,
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
		{"OpenRouter", "OpenRouter \u2726 [account]"},
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

// pi v1.5.2 (b4afdddf) components/oauth-selector.ts marks a preferred partner
// provider with " ✦" (U+2726, single-width in every terminal), on the selected
// row and the unselected ones alike.
func TestAuthSelectorMarksPreferredPartners(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	providers := []AuthSelectorProvider{
		{ID: "openrouter", Name: "OpenRouter", AuthType: "oauth"},
		{ID: "anthropic", Name: "Anthropic", AuthType: "oauth"},
	}
	selector := NewOAuthSelectorComponent("login", providers, nil, nil, "")
	text := tui.StripTerminalSequences(strings.Join(selector.Render(120), "\n"))
	var partnerRow, otherRow string
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.Contains(line, "OpenRouter"):
			partnerRow = line
		case strings.Contains(line, "Anthropic"):
			otherRow = line
		}
	}
	if !strings.Contains(partnerRow, "\u2726") {
		t.Fatalf("preferred partner row has no marker: %q", partnerRow)
	}
	if strings.Contains(otherRow, "\u2726") {
		t.Fatalf("non-partner row carries the marker: %q", otherRow)
	}
}
