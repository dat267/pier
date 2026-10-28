package ai

import (
	"context"
	"testing"
)

// pi v1.4.0 (4a080252), models.ts createProvider publishes a fetched overlay
// through context.publish's Update closure. Go must synchronize that assignment
// with concurrent public GetModels calls, just as it does restored overlays.
func TestDynamicProviderRefreshSynchronizesCatalogReads(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	provider := CreateProvider(CreateProviderOptions{
		ID:     "test-dynamic",
		Single: capturingStreams{},
		FetchModels: func(*RefreshModelsContext) ([]*Model, error) {
			close(started)
			<-release
			return []*Model{{ID: "fresh", Provider: "test-dynamic"}}, nil
		},
	})
	refreshDone := make(chan error, 1)
	readDone := make(chan struct{})
	go func() {
		refreshDone <- provider.RefreshModels(&RefreshModelsContext{
			Ctx:          context.Background(),
			AllowNetwork: true,
			Publish: func(publication ModelsPublication) bool {
				publication.Update()
				return true
			},
		})
	}()
	<-started
	go func() {
		provider.GetModels()
		close(readDone)
	}()
	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	<-readDone
	models := provider.GetModels()
	if len(models) != 1 || models[0].ID != "fresh" {
		t.Fatalf("refreshed catalog = %+v; want fresh model", models)
	}
}
