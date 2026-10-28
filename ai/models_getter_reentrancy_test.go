package ai

import (
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), models.ts Models.getModels directly delegates to
// each provider's getter; that getter can also read the registry's GetProvider.
func TestModelsGetterCanReadRegistry(t *testing.T) {
	for _, id := range []string{"test-reentrant", ""} {
		t.Run("provider="+id, func(t *testing.T) {
			registry := CreateModels(nil)
			registry.SetProvider(CreateProvider(CreateProviderOptions{
				ID:     "test-reentrant",
				Single: capturingStreams{},
				ModelsGetter: func() []*Model {
					if registry.GetProvider("test-reentrant") == nil {
						return nil
					}
					return []*Model{{ID: "model", Provider: "test-reentrant"}}
				},
			}))
			done := make(chan []*Model, 1)
			go func() { done <- registry.GetModels(id) }()
			select {
			case models := <-done:
				if len(models) != 1 || models[0].ID != "model" {
					t.Fatalf("models = %+v; want model", models)
				}
			case <-time.After(time.Second):
				t.Fatal("model getter deadlocked reading its registry")
			}
		})
	}
}
