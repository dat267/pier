package coding

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts publishes an overlay
// through context.publish's update callback. Go model readers and refresh
// workers run concurrently, so wrapper overlays need the same synchronization
// as ai.CreateProvider's dynamic catalog. Publication callbacks may read models.
func TestCatalogWrapperRefreshSynchronizesReads(t *testing.T) {
	for _, personalized := range []bool{false, true} {
		t.Run(fmt.Sprintf("personalized=%t", personalized), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			fetch := func(context.Context, *http.Request) (*http.Response, error) {
				close(started)
				<-release
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`[{"id":"fresh","type":"image"}]`)),
				}, nil
			}
			base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p"})
			provider := WithRemoteCatalog(base, "https://catalog.invalid", 0, false, fetch)
			if personalized {
				provider = WithPersonalizedCatalog(base, func(models []*ai.Model, _ *ai.RefreshModelsContext) ([]*ai.Model, error) {
					return models, nil
				}, "https://catalog.invalid", 0, false, fetch)
			}
			refreshDone := make(chan error, 1)
			go func() {
				refreshDone <- provider.RefreshModels(&ai.RefreshModelsContext{
					Ctx: context.Background(), AllowNetwork: true,
					Stored: &ai.ModelsStoreEntry{Models: []*ai.Model{{ID: "restored", Provider: "p"}}},
					Publish: func(publication ai.ModelsPublication) bool {
						if publication.Update != nil {
							publication.Update()
						}
						provider.GetAllModels() // reentrant publication control
						return true
					},
				})
			}()
			<-started
			readDone := make(chan struct{})
			go func() {
				provider.GetAllModels()
				close(readDone)
			}()
			close(release)
			err := <-refreshDone
			<-readDone
			if err != nil {
				t.Fatal(err)
			}
			models := provider.GetAllModels()
			if len(models) != 1 || models[0].ID != "fresh" || models[0].Type != "image" {
				t.Fatalf("refreshed models = %+v, want fresh image model", models)
			}
		})
	}
}
