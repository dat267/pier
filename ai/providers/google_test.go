package providers

import (
	"testing"

	"github.com/dat267/pier/ai"
)

type googleAuthCtx map[string]string

func (c googleAuthCtx) Env(name string) (string, bool) { v, ok := c[name]; return v, ok }
func (c googleAuthCtx) FileExists(string) bool         { return false }

// pi v1.5.2 (b4afdddf) providers/google.ts:11 lists exactly one env var for the
// Gemini key, and env-api-keys.ts:95 maps the provider to that same name. The port
// also accepted GOOGLE_GENERATIVE_AI_API_KEY, which the reference never reads, so a
// user with only that variable set was authenticated here and refused there.
func TestGoogleProviderUsesOnlyTheReferenceEnvVar(t *testing.T) {
	provider := GoogleProvider()
	if provider == nil || provider.Auth.APIKey == nil {
		t.Fatal("no api-key auth")
	}
	resolved, err := provider.Auth.APIKey.Resolve(ai.AuthResolveInput{Ctx: googleAuthCtx{"GEMINI_API_KEY": "k"}})
	if err != nil || resolved == nil {
		t.Fatalf("GEMINI_API_KEY must resolve: %v", err)
	}
	if resolved.Source != "GEMINI_API_KEY" {
		t.Fatalf("source = %q", resolved.Source)
	}
	extra, err := provider.Auth.APIKey.Resolve(ai.AuthResolveInput{Ctx: googleAuthCtx{"GOOGLE_GENERATIVE_AI_API_KEY": "k"}})
	if err != nil {
		t.Fatal(err)
	}
	if extra != nil {
		t.Fatalf("GOOGLE_GENERATIVE_AI_API_KEY resolved as %q, but the reference does not accept it", extra.Source)
	}
}
