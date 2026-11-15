package coding

import (
	ctxpkg "context"
	"sync"
	"testing"
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
				if err := runtime.SetRuntimeAPIKey(providerID, "sk-"+providerID, ctx); err != nil {
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
