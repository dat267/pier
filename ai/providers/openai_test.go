package providers

import "testing"

// TestOpenAIProviderChatGPTOAuth pins that the OpenAI provider advertises the
// ChatGPT-subscription OAuth alongside its api key (upstream openaiProvider).
func TestOpenAIProviderChatGPTOAuth(t *testing.T) {
	provider := OpenAIProvider()
	if provider.Auth.APIKey == nil {
		t.Fatal("openai has no api-key auth")
	}
	if provider.Auth.OAuth == nil {
		t.Fatal("openai has no oauth auth")
	}
	if provider.Auth.OAuth.Name != "OpenAI (ChatGPT subscription)" {
		t.Fatalf("oauth name = %q", provider.Auth.OAuth.Name)
	}
	if provider.Auth.OAuth.LoginLabel != "Sign in with ChatGPT" || !provider.Auth.OAuth.IsSubscription {
		t.Fatalf("oauth = %+v", provider.Auth.OAuth)
	}
}
