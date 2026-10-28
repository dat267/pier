package ai

import (
	"context"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), models.ts createProvider invokes context.publish
// outside model-access guards; its synchronous Update closure restores models.
// A host publication callback may read the provider's catalog after updating it.
func TestStoredProviderPublicationCanReadModels(t *testing.T) {
	provider := CreateProvider(CreateProviderOptions{
		ID:     "test-dynamic",
		Single: capturingStreams{},
		FetchModels: func(*RefreshModelsContext) ([]*Model, error) {
			t.Error("offline restore must not fetch")
			return nil, nil
		},
	})
	done := make(chan []*Model, 1)
	go func() {
		var published []*Model
		provider.RefreshModels(&RefreshModelsContext{
			Ctx: context.Background(),
			Stored: &ModelsStoreEntry{Models: []*Model{
				{ID: "restored", Provider: "test-dynamic"},
			}},
			Publish: func(publication ModelsPublication) bool {
				publication.Update()
				published = provider.GetModels()
				return true
			},
		})
		done <- published
	}()
	select {
	case models := <-done:
		if len(models) != 1 || models[0].ID != "restored" {
			t.Fatalf("published catalog = %+v; want restored model", models)
		}
	case <-time.After(time.Second):
		t.Fatal("publication callback deadlocked reading the provider's catalog")
	}
}
