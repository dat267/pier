package coding

import (
	ctxpkg "context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Round 100 tests: the ModelRuntime composition/availability/credential
// synchronization and ai.Models login/logout.

// pi v1.4.0 credential-print.ts passes minOAuthValidityMs when it is zero.
func TestModelRuntimeGetAuthPreservesExplicitZeroOAuthValidity(t *testing.T) {
	store := newMemoryCredentialStore()
	store.entries["p"] = &ai.Credential{Type: ai.CredentialOAuth, OAuth: &ai.OAuthCredential{
		Refresh: "r1", Access: "a1", Expires: time.Now().Add(time.Minute).UnixMilli()}}
	provider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "p", Name: "P",
		Auth: ai.ProviderAuth{OAuth: &ai.OAuthAuth{
			Refresh: func(ctxpkg.Context, *ai.OAuthCredential) (*ai.OAuthCredential, error) {
				return &ai.OAuthCredential{
					Refresh: "r2", Access: "a2", Expires: time.Now().Add(4 * time.Minute).UnixMilli()}, nil
			},
			ToAuth: func(credential *ai.OAuthCredential) (*ai.ModelAuth, error) {
				return &ai.ModelAuth{APIKey: credential.Access}, nil
			},
		}},
	})
	models := ai.CreateModels(&ai.CreateModelsOptions{Credentials: store})
	models.SetProvider(provider)
	runtime := &ModelRuntime{models: models}
	zero := int64(0)
	_, err := runtime.GetAuth("p", &ModelRuntimeAuthOverrides{MinOAuthValidityMS: &zero})
	if err == nil || err.Error() != "OAuth refresh returned a token that expires too soon for p" {
		t.Fatalf("GetAuth error = %v, want too-soon error for explicit zero validity", err)
	}
}

// pi v1.4.0 packages/coding-agent/src/cli/auth-check.ts classifies refresh errors as invalid_state.
func TestCheckProviderAuthRefreshFailureIsInvalid(t *testing.T) {
	store := newMemoryCredentialStore()
	store.entries["p"] = &ai.Credential{Type: ai.CredentialOAuth, OAuth: &ai.OAuthCredential{
		Refresh: "r1", Access: "a1", Expires: time.Now().Add(time.Minute).UnixMilli()}}
	provider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "p", Name: "P",
		Auth: ai.ProviderAuth{OAuth: &ai.OAuthAuth{
			Refresh: func(ctxpkg.Context, *ai.OAuthCredential) (*ai.OAuthCredential, error) {
				return nil, errors.New("refresh failed")
			},
			ToAuth: func(credential *ai.OAuthCredential) (*ai.ModelAuth, error) {
				return &ai.ModelAuth{APIKey: credential.Access}, nil
			},
		}},
	})
	models := ai.CreateModels(&ai.CreateModelsOptions{Credentials: store})
	models.SetProvider(provider)
	runtime := &ModelRuntime{models: models, config: &ModelConfig{}}
	providerID := "p"
	result, err := CheckProviderAuth(&Args{Provider: &providerID}, runtime, true)
	if err != nil {
		t.Fatalf("CheckProviderAuth error = %v, want classified result", err)
	}
	if result.Status != AuthCheckInvalid || result.Reason != AuthReasonInvalidState {
		t.Fatalf("CheckProviderAuth = %+v, want invalid_state", result)
	}
}

func TestModelsLoginLogout(t *testing.T) {
	ctx := ctxpkg.Background()
	store := newMemoryCredentialStore()
	var prompted []string
	modelProvider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "p", Name: "P", Models: nil,
		Auth: ai.ProviderAuth{
			APIKey: &ai.ApiKeyAuth{
				Login: func(interaction *ai.AuthInteraction) (*ai.ApiKeyCredential, error) {
					key, err := interaction.Prompt(ai.AuthPrompt{Type: ai.AuthPromptSecret, Message: "Enter API key"})
					if err != nil {
						return nil, err
					}
					prompted = append(prompted, key)
					return &ai.ApiKeyCredential{Key: key, Env: ai.ProviderEnv{"REGION": "eu"}}, nil
				},
			},
			OAuth: &ai.OAuthAuth{
				Name: "P OAuth",
				Login: func(interaction *ai.AuthInteraction) (*ai.OAuthCredential, error) {
					return &ai.OAuthCredential{
						Access: "access-1", Refresh: "refresh-1", Expires: 1}, nil
				},
			},
		},
	})
	models := ai.CreateModels(&ai.CreateModelsOptions{Credentials: store})
	models.SetProvider(modelProvider)

	credential, err := models.Login("p", ai.AuthTypeAPIKey, &ai.AuthInteraction{
		Ctx: ctx,
		Prompt: func(prompt ai.AuthPrompt) (string, error) {
			return "sk-entered", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.APIKey == nil || credential.APIKey.Key != "sk-entered" || credential.APIKey.Env["REGION"] != "eu" {
		t.Fatalf("credential = %+v", credential)
	}
	if len(prompted) != 1 || prompted[0] != "sk-entered" {
		t.Fatalf("prompted = %v", prompted)
	}
	stored, _ := store.Read(ctx, "p")
	if stored == nil || stored.APIKey.Key != "sk-entered" {
		t.Fatalf("stored = %+v", stored)
	}

	// OAuth login stores an OAuth credential.
	credential, err = models.Login("p", ai.AuthTypeOAuth, &ai.AuthInteraction{Ctx: ctx})
	if err != nil || credential.Type != ai.CredentialOAuth || credential.OAuth.Access != "access-1" {
		t.Fatalf("credential = %+v err = %v", credential, err)
	}
	stored, _ = store.Read(ctx, "p")
	if stored == nil || stored.Type != ai.CredentialOAuth {
		t.Fatalf("stored = %+v", stored)
	}

	// An unknown provider, a method without login, and store failures error.
	if _, err := models.Login("nope", ai.AuthTypeAPIKey, &ai.AuthInteraction{Ctx: ctx}); err == nil ||
		err.Error() != "Unknown provider: nope" {
		t.Fatalf("err = %v", err)
	}
	noLogin := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "nologin", Name: "No Login", Models: nil,
		Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{Name: "env only"}},
	})
	models.SetProvider(noLogin)
	if _, err := models.Login("nologin", ai.AuthTypeAPIKey, &ai.AuthInteraction{Ctx: ctx}); err == nil ||
		err.Error() != "No Login does not support api_key login" {
		t.Fatalf("err = %v", err)
	}
	if _, err := models.Login("nologin", ai.AuthTypeOAuth, &ai.AuthInteraction{Ctx: ctx}); err == nil ||
		err.Error() != "No Login does not support oauth login" {
		t.Fatalf("err = %v", err)
	}

	store.modifyErr = errors.New("disk full")
	if _, err := models.Login("p", ai.AuthTypeAPIKey, &ai.AuthInteraction{
		Ctx:    ctx,
		Prompt: func(ai.AuthPrompt) (string, error) { return "k", nil },
	}); err == nil || !strings.Contains(err.Error(), "Credential store modify failed for p") {
		t.Fatalf("err = %v", err)
	}
	store.modifyErr = nil

	// A cancelled context aborts before the mutation.
	cancelled, cancel := ctxpkg.WithCancel(ctx)
	cancel()
	if _, err := models.Login("p", ai.AuthTypeAPIKey, &ai.AuthInteraction{Ctx: cancelled}); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}

	// Logout deletes; store failures wrap.
	if err := models.Logout(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if stored, _ := store.Read(ctx, "p"); stored != nil {
		t.Fatalf("stored = %+v", stored)
	}
	store.deleteErr = errors.New("locked")
	if err := models.Logout(ctx, "p"); err == nil || !strings.Contains(err.Error(), "Credential store delete failed for p") {
		t.Fatalf("err = %v", err)
	}
	store.deleteErr = nil
}

func TestModelRuntimeGetAuthForModelHeaders(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(modelsPath, []byte(`{"providers":{"alpha":{
		"baseUrl":"https://beta.example.com",
		"headers":{"X-Provider":"p"},
		"models":[{"id":"alpha-model","headers":{"X-Model":"m"}}]
	}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		ModelsPath:      modelsPath,
		RefreshOnCreate: new(false),
		Credentials:     newMemoryCredentialStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	stub := stubProvider("alpha")
	runtime.defaults["alpha"] = stub
	runtime.builtins["alpha"] = stub
	runtime.recomposeProvider("alpha")

	model := runtime.GetModel("alpha", "alpha-model")
	if model == nil {
		t.Fatal("model missing")
	}
	// Provider headers are not part of the model header set; the model's own
	// headers are.
	resolution, err := runtime.GetAuthForModel(model, &ModelRuntimeAuthOverrides{APIKey: "sk-1"})
	if err != nil || resolution == nil {
		t.Fatalf("resolution = %+v err = %v", resolution, err)
	}
	if resolution.Auth.APIKey != "sk-1" || resolution.Auth.Headers["X-Model"] == nil ||
		*resolution.Auth.Headers["X-Model"] != "m" {
		t.Fatalf("resolution = %+v", resolution)
	}
	// Provider headers arrive through the composed auth path (upstream merges
	// them in composeApiKeyAuth), while model headers are merged here.
	if resolution.Auth.Headers["X-Provider"] == nil || *resolution.Auth.Headers["X-Provider"] != "p" {
		t.Fatalf("provider headers missing: %+v", resolution.Auth.Headers)
	}
}
