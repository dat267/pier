package coding

import (
	ctxpkg "context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// A background refresh (interactive mode fires them from goroutines) reloads
// models.json while other goroutines read the runtime: GetError reads the
// composition errors and the compatibility fallback reads the provider config.
// Every field those readers touch must be synchronized.
func TestModelRuntimeRefreshDoesNotRaceReaders(t *testing.T) {
	runtime := runtimeWithProviders(t, stubProvider("alpha"), stubProvider("beta"))
	model := runtime.GetModel("alpha", "alpha-model")
	ctx := ctxpkg.Background()
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		for range 20 {
			_, _ = runtime.Refresh(ctx, &ModelsRefreshCallOptions{})
		}
	}()
	go func() {
		defer wait.Done()
		for range 20 {
			_ = runtime.GetError()
			_, _ = runtime.GetCompatibilityRequestConfig(model)
		}
	}()
	go func() {
		defer wait.Done()
		for range 20 {
			_ = runtime.GetModels("alpha")
			_ = runtime.HasConfiguredAuth("alpha")
		}
	}()
	wait.Wait()
}

// Credential operations are serialized per provider, so two providers can write
// their keys at once. Both recompose their provider and record the outcome that
// GetError reads.
func TestModelRuntimeConcurrentCredentialWritesDoNotRaceReaders(t *testing.T) {
	runtime := runtimeWithProviders(t, stubProvider("alpha"), stubProvider("beta"))
	ctx := ctxpkg.Background()
	var wait sync.WaitGroup
	wait.Add(4)
	for _, providerID := range []string{"alpha", "beta"} {
		go func() {
			defer wait.Done()
			for range 10 {
				if err := runtime.SetRuntimeAPIKey(ctx, providerID, "sk-"+providerID); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	go func() {
		defer wait.Done()
		for range 20 {
			_ = runtime.GetError()
		}
	}()
	go func() {
		defer wait.Done()
		for range 20 {
			_ = runtime.GetProviderAuthStatus("alpha")
		}
	}()
	wait.Wait()
}

// A cancelled refresh must not overwrite a good snapshot with emptiness.
func TestModelRuntimeCancelledRefreshKeepsSnapshot(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(modelsPath, []byte(`{"providers":{"alpha":{
		"baseUrl":"https://alpha.example.com",
		"models":[{"id":"alpha-model"}]
	}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	creds := newMemoryCredentialStore()
	_, _ = creds.Modify(ctxpkg.Background(), "alpha", func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-1"}}, nil
	})
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		ModelsPath: modelsPath, RefreshOnCreate: new(false), Credentials: creds,
	})
	if err != nil {
		t.Fatal(err)
	}
	stub := stubProvider("alpha")
	runtime.defaults["alpha"] = stub
	runtime.builtins["alpha"] = stub
	runtime.recomposeProvider("alpha")

	runtime.queueAvailabilityRefresh(ctxpkg.Background())
	before := runtime.GetAvailableSnapshot()
	if len(before) == 0 {
		t.Fatal("the fixture yielded no available models, so the overwrite cannot be detected")
	}
	configuredBefore := len(runtime.snapshot.ConfiguredProviders)

	ctx, cancel := ctxpkg.WithCancel(ctxpkg.Background())
	cancel()
	runtime.queueAvailabilityRefresh(ctx)
	after := runtime.GetAvailableSnapshot()
	if len(after) != len(before) || len(runtime.snapshot.ConfiguredProviders) != configuredBefore {
		t.Fatalf("a cancelled refresh changed the snapshot: %d -> %d models, configured %d -> %d",
			len(before), len(after), configuredBefore, len(runtime.snapshot.ConfiguredProviders))
	}
}
