package ai

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
)

// Port of the auth-resolution and models-runtime subset of
// packages/ai/test/providers.test.ts, plus calculateCost/thinking-level
// units from models.ts. Builtin-catalog cases (models.dev data) await the
// catalog port.

type fakeAuthContext struct {
	env   map[string]string
	files []string
}

func (f *fakeAuthContext) Env(name string) (string, bool) {
	v, ok := f.env[name]
	return v, ok
}

func (f *fakeAuthContext) FileExists(path string) bool {
	return slices.Contains(f.files, path)
}

func TestEnvApiKeyAuthPrefersStoredKeyAndFallsThroughEnvVarsInOrder(t *testing.T) {
	// Upstream: "prefers the stored credential key and falls back through
	// env vars in order".
	auth := EnvApiKeyAuth("Test", []string{"TEST_A", "TEST_B"})
	ctx := &fakeAuthContext{env: map[string]string{"TEST_A": "", "TEST_B": "b-value"}}

	// No credential: falls to the first set env var.
	result, err := auth.Resolve(AuthResolveInput{Ctx: ctx, Ctx2: bgCtx()})
	if err != nil || result == nil || result.Auth.APIKey != "b-value" || result.Source != "TEST_B" {
		t.Fatalf("ambient resolve = %+v, %v", result, err)
	}

	// Stored credential key wins.
	result, err = auth.Resolve(AuthResolveInput{
		Ctx:        ctx,
		Credential: &ApiKeyCredential{Key: "stored", Env: ProviderEnv{"PROVIDER_ID": "123"}},
		Ctx2:       bgCtx(),
	})
	if err != nil || result == nil || result.Auth.APIKey != "stored" || result.Source != "stored credential" {
		t.Fatalf("stored resolve = %+v, %v", result, err)
	}
	if result.Env["PROVIDER_ID"] != "123" {
		t.Fatalf("env = %v", result.Env)
	}

	// Unconfigured: undefined (no env var set at all).
	emptyCtx := &fakeAuthContext{env: map[string]string{}}
	resolution, err := ResolveProviderAuth("p", ProviderAuth{APIKey: auth}, NewInMemoryCredentialStore(), emptyCtx, nil)
	if err != nil || resolution != nil {
		t.Fatalf("unconfigured = %+v, %v", resolution, err)
	}
}

func TestResolveProviderAuthStoredCredentialOwnsProvider(t *testing.T) {
	// Upstream: "A stored credential owns the provider: ambient/env is
	// consulted only when nothing is stored."
	auth := EnvApiKeyAuth("Test", []string{"TEST_A"})
	ctx := &fakeAuthContext{env: map[string]string{"TEST_A": "ambient"}}
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "stored"}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveProviderAuth("p", ProviderAuth{APIKey: auth}, store, ctx, nil)
	if err != nil || resolution.Auth.APIKey != "stored" {
		t.Fatalf("resolution = %+v, %v", resolution, err)
	}

	// API-key override beats the stored credential.
	resolution, err = ResolveProviderAuth("p", ProviderAuth{APIKey: auth}, store, ctx,
		&AuthResolutionOverrides{APIKey: "override"})
	if err != nil || resolution.Auth.APIKey != "override" {
		t.Fatalf("override = %+v, %v", resolution, err)
	}

	// Env overrides overlay the auth context.
	resolution, err = ResolveProviderAuth("p", ProviderAuth{APIKey: auth}, NewInMemoryCredentialStore(), ctx,
		&AuthResolutionOverrides{Env: ProviderEnv{"TEST_A": "overridden"}})
	if err != nil || resolution.Auth.APIKey != "overridden" {
		t.Fatalf("env override = %+v, %v", resolution, err)
	}
}

func TestResolveProviderAuthExplicitEmptyAPIKeyOverridesStoredOAuth(t *testing.T) {
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{Access: "stored-oauth", Expires: time.Now().Add(time.Hour).UnixMilli()}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}
	auth := ProviderAuth{
		OAuth: &OAuthAuth{ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
			return &ModelAuth{APIKey: credential.Access}, nil
		}},
		APIKey: &ApiKeyAuth{Resolve: func(input AuthResolveInput) (*AuthResult, error) {
			return nil, nil
		}},
	}
	resolution, err := ResolveProviderAuth("p", auth, store, &fakeAuthContext{env: map[string]string{}}, &AuthResolutionOverrides{APIKey: "", APIKeySet: true})
	if err != nil {
		t.Fatal(err)
	}
	if resolution != nil {
		t.Fatalf("explicit empty API key override returned stored OAuth auth: %+v", resolution)
	}
}

func TestModelsApplyAuthPreservesExplicitEmptyAPIKey(t *testing.T) {
	models := CreateModels(&CreateModelsOptions{Credentials: staticCredentialStore("p", "stored-key"), AuthContext: &fakeAuthContext{env: map[string]string{"KEY": "ambient-key"}}})
	models.SetProvider(&Provider{ID: "p", Auth: ProviderAuth{APIKey: EnvApiKeyAuth("key", []string{"KEY"})}})
	model, options, err := models.applyAuth(&Model{ID: "m", Provider: "p"}, &ModelsStreamOptions{StreamOptions: StreamOptions{APIKey: "", APIKeySet: true}})
	if err != nil {
		t.Fatal(err)
	}
	if model == nil || options.APIKey != "" {
		t.Fatalf("applied options = %+v, want explicit empty API key", options)
	}
}

func TestModelsLoginForwardsApplicationName(t *testing.T) {
	var received string
	models := CreateModels(nil)
	models.SetProvider(&Provider{
		ID: "p", Name: "provider",
		Auth: ProviderAuth{OAuth: &OAuthAuth{
			Login: func(interaction *AuthInteraction) (*OAuthCredential, error) {
				received = interaction.AgentName
				return &OAuthCredential{}, nil
			},
		}},
	})

	interaction := &AuthInteraction{AgentName: "ExampleApp"}
	if _, err := models.Login("p", AuthTypeOAuth, interaction); err != nil {
		t.Fatal(err)
	}
	if received != "ExampleApp" {
		t.Fatalf("OAuth login received AgentName %q; want ExampleApp", received)
	}
}

// Port of pi v1.4.0 packages/ai/src/auth/resolve.ts resolveStoredOAuth:
// an explicitly supplied zero still activates its post-refresh five-minute floor.
func TestResolveStoredOAuthExplicitZeroStillAppliesPostRefreshFloor(t *testing.T) {
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Refresh: "r1", Access: "a1", Expires: nowUnixMilli() + 60_000,
		}}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}
	oauth := &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(*OAuthCredential, context.Context) (*OAuthCredential, error) {
			return &OAuthCredential{OAuthCredentials{
				Refresh: "r2", Access: "a2", Expires: nowUnixMilli() + 4*60_000,
			}}, nil
		},
		ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
			return &ModelAuth{APIKey: credential.Access}, nil
		},
	}
	_, err := ResolveProviderAuth("p", ProviderAuth{OAuth: oauth}, store, &fakeAuthContext{}, &AuthResolutionOverrides{
		MinOAuthValidityMS:    0,
		MinOAuthValidityMSSet: true,
	})
	if err == nil {
		t.Fatal("explicit zero minimum accepted a refreshed token inside the five-minute floor")
	}
}

// Port of pi v1.4.0 packages/ai/src/auth/resolve.ts resolveStoredOAuth:
// an unrepresentably large expiry threshold must not wrap into a valid token.
func TestResolveStoredOAuthMaxValidityDoesNotWrap(t *testing.T) {
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Refresh: "r1", Access: "a1", Expires: nowUnixMilli() + 60*60_000,
		}}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}
	refreshes := 0
	oauth := &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(*OAuthCredential, context.Context) (*OAuthCredential, error) {
			refreshes++
			return &OAuthCredential{OAuthCredentials{
				Refresh: "r2", Access: "a2", Expires: nowUnixMilli() + 10*60*60_000,
			}}, nil
		},
		ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
			return &ModelAuth{APIKey: credential.Access}, nil
		},
	}
	maximum := int64(1<<63 - 1)
	_, err := ResolveProviderAuth("p", ProviderAuth{OAuth: oauth}, store, &fakeAuthContext{}, &AuthResolutionOverrides{
		MinOAuthValidityMS:    maximum,
		MinOAuthValidityMSSet: true,
	})
	if err == nil || err.Error() != "OAuth refresh returned a token that expires too soon for p" {
		t.Fatalf("GetAuth error = %v, want too-soon error for unrepresentable validity", err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
}

func TestResolveStoredOAuthRefreshesUnderLock(t *testing.T) {
	// Upstream: double-checked locking — tokens with less than five minutes
	// remaining lock, re-check, refresh once, persist before release.
	refreshes := 0
	oauth := &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(credential *OAuthCredential, ctx context.Context) (*OAuthCredential, error) {
			refreshes++
			return &OAuthCredential{OAuthCredentials{
				Refresh: "r2", Access: "a2",
				Expires: nowUnixMilli() + 3600_000,
			}}, nil
		},
		ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
			return &ModelAuth{APIKey: credential.Access}, nil
		},
	}
	ctx := &fakeAuthContext{}
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Refresh: "r1", Access: "a1", Expires: nowUnixMilli() + 60_000, // expires soon
		}}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}

	resolution, err := ResolveProviderAuth("p", ProviderAuth{OAuth: oauth}, store, ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d; want 1", refreshes)
	}
	if resolution.Auth.APIKey != "a2" || resolution.Source != "OAuth" {
		t.Fatalf("resolution = %+v", resolution)
	}
	// The rotated credential was persisted.
	stored, _ := store.Read("p", bgCtx())
	if stored.OAuth.Access != "a2" {
		t.Fatalf("stored = %+v", stored.OAuth)
	}

	// A second resolution within validity does not refresh again.
	if _, err := ResolveProviderAuth("p", ProviderAuth{OAuth: oauth}, store, ctx, nil); err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes after reuse = %d; want 1", refreshes)
	}
}

func TestResolveStoredOAuthPersistsRefreshStartedBeforeCancellation(t *testing.T) {
	// Upstream bde882c74: a provider that rotates refresh tokens has already
	// invalidated the old one by the time the caller cancels, so a refresh
	// that started must complete and persist or the next request fails with
	// refresh_token_invalidated. The caller still observes the cancellation.
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Refresh: "old-refresh", Access: "old", Expires: 0, // expired
		}}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oauth := &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(credential *OAuthCredential, refreshCtx context.Context) (*OAuthCredential, error) {
			// The provider rotates the token, then the caller cancels before
			// the refresh returns.
			cancel()
			return &OAuthCredential{OAuthCredentials{
				Refresh: "new-refresh", Access: "new", Expires: nowUnixMilli() + 3600_000,
			}}, nil
		},
		ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
			return &ModelAuth{APIKey: credential.Access}, nil
		},
	}

	if _, err := ResolveProviderAuth("p", ProviderAuth{OAuth: oauth}, store, &fakeAuthContext{},
		&AuthResolutionOverrides{Ctx: ctx}); err == nil {
		t.Fatal("expected the cancellation to surface")
	}

	stored, err := store.Read("p", bgCtx())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Type != CredentialOAuth || stored.OAuth.Refresh != "new-refresh" {
		t.Fatalf("rotated credential not persisted: %+v", stored)
	}
}

func TestModelsRefreshPersistsOAuthRefreshStartedBeforeCancellation(t *testing.T) {
	// Upstream bde882c74: a model-list refresh that is cancelled (or
	// superseded) while an OAuth refresh is in flight must still persist the
	// rotated credential.
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p1", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Refresh: "old-refresh", Access: "old", Expires: 0, // expired
		}}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oauth := &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(credential *OAuthCredential, refreshCtx context.Context) (*OAuthCredential, error) {
			cancel()
			return &OAuthCredential{OAuthCredentials{
				Refresh: "new-refresh", Access: "new", Expires: nowUnixMilli() + 3600_000,
			}}, nil
		},
	}
	models := CreateModels(&CreateModelsOptions{Credentials: store, AuthContext: &fakeAuthContext{}})
	models.SetProvider(&Provider{
		ID:            "p1",
		Auth:          ProviderAuth{OAuth: oauth},
		RefreshModels: func(*RefreshModelsContext) error { return nil },
	})

	result := models.Refresh(&ModelsRefreshOptions{Ctx: ctx})
	if !result.Aborted {
		t.Fatalf("refresh = %+v; want aborted", result)
	}

	stored, err := store.Read("p1", bgCtx())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Type != CredentialOAuth || stored.OAuth.Refresh != "new-refresh" {
		t.Fatalf("rotated credential not persisted: %+v", stored)
	}
}

func TestCredentialJSONRoundTrip(t *testing.T) {
	// The auth.json shape: type-tagged, one credential per provider.
	apiKey := &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "k", Env: ProviderEnv{"X": "1"}}}
	enc, err := MarshalJSON(apiKey)
	if err != nil {
		t.Fatal(err)
	}
	var back Credential
	if err := json.Unmarshal(enc, &back); err != nil {
		t.Fatal(err)
	}
	if back.Type != CredentialAPIKey || back.APIKey.Key != "k" || back.APIKey.Env["X"] != "1" {
		t.Fatalf("api_key round trip = %+v (%s)", back, enc)
	}

	oauth := &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
		Refresh: "r", Access: "a", Expires: 123,
		Extra: map[string]json.RawMessage{"plan": json.RawMessage(`"pro"`)},
	}}}
	enc, err = MarshalJSON(oauth)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"oauth","refresh":"r","access":"a","expires":123,"plan":"pro"}`
	if string(enc) != want {
		t.Fatalf("oauth = %s; want %s", enc, want)
	}
	var back2 Credential
	if err := json.Unmarshal(enc, &back2); err != nil {
		t.Fatal(err)
	}
	if back2.OAuth.Refresh != "r" || string(back2.OAuth.Extra["plan"]) != `"pro"` {
		t.Fatalf("oauth round trip = %+v", back2.OAuth)
	}
}

func (c capturingStreams) Stream(m *Model, t TranscriptContext, o *StreamOptions) *AssistantMessageEventStream {
	return c.stream(m, t, o)
}

// pi v1.5.2 (b4afdddf) models.ts added `refreshModelsAuth: "configured"`: a
// provider whose catalog is served elsewhere (the pi.dev overlay) refreshes as
// long as it is configured, without refreshing a stale OAuth credential, so a
// dead login cannot block or fail the catalog refresh.
func TestModelsRefreshConfiguredAuthSkipsOAuthRefresh(t *testing.T) {
	store := NewInMemoryCredentialStore()
	if _, err := store.Modify("p1", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Refresh: "dead-refresh", Access: "dead", Expires: 0, // expired
		}}}, nil
	}, bgCtx()); err != nil {
		t.Fatal(err)
	}
	oauth := &OAuthAuth{
		Name: "Test OAuth",
		Refresh: func(*OAuthCredential, context.Context) (*OAuthCredential, error) {
			return nil, errors.New("refresh failed")
		},
	}
	var gotCredential *Credential
	refreshCalled := false
	models := CreateModels(&CreateModelsOptions{Credentials: store, AuthContext: &fakeAuthContext{}})
	models.SetProvider(&Provider{
		ID:                "p1",
		Auth:              ProviderAuth{OAuth: oauth},
		RefreshModelsAuth: "configured",
		RefreshModels: func(refresh *RefreshModelsContext) error {
			if refresh.AllowNetwork {
				refreshCalled = true
				gotCredential = refresh.Credential
			}
			return nil
		},
	})

	result := models.Refresh(&ModelsRefreshOptions{Ctx: bgCtx()})
	if err := result.Errors["p1"]; err != nil {
		t.Fatalf("refresh error = %v, want none: a configured catalog must not need the credential", err)
	}
	if !refreshCalled {
		t.Fatal("the network phase did not run")
	}
	if gotCredential != nil {
		t.Fatalf("network phase got credential %+v, want none", gotCredential)
	}
}

// The other half of upstream's resolveNetworkRefresh: a "configured" provider
// that is not configured at all still skips the network phase.
func TestModelsRefreshConfiguredAuthSkipsUnconfiguredProvider(t *testing.T) {
	store := NewInMemoryCredentialStore()
	refreshCalled := false
	models := CreateModels(&CreateModelsOptions{Credentials: store, AuthContext: &fakeAuthContext{}})
	models.SetProvider(&Provider{
		ID:                "p1",
		Auth:              ProviderAuth{OAuth: &OAuthAuth{Name: "Test OAuth"}},
		RefreshModelsAuth: "configured",
		RefreshModels: func(refresh *RefreshModelsContext) error {
			if refresh.AllowNetwork {
				refreshCalled = true
			}
			return nil
		},
	})

	result := models.Refresh(&ModelsRefreshOptions{Ctx: bgCtx()})
	if err := result.Errors["p1"]; err != nil {
		t.Fatalf("refresh error = %v", err)
	}
	if refreshCalled {
		t.Fatal("the network phase ran for an unconfigured provider")
	}
}
