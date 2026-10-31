package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts getAllModels in
// both wrappers retains image/classifier models. The upstream tests also pin
// restoring a personalized image-only list without reviving builtin models.
func TestCatalogWrappersExposeCompleteModelLists(t *testing.T) {
	for _, personalized := range []bool{false, true} {
		for _, network := range []bool{false, true} {
			t.Run(fmt.Sprintf("personalized=%t/network=%t", personalized, network), func(t *testing.T) {
				base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p", Models: []*ai.Model{
					{ID: "builtin-chat", Provider: "p"},
					{ID: "builtin-image", Provider: "p", Type: "image"},
					{ID: "builtin-classifier", Provider: "p", Type: "classifier"},
				}})
				const payload = `[{"id":"shared","type":"chat"},{"id":"shared","type":"image"},{"id":"decision","type":"classifier"}]`
				stored, err := ParseRemoteCatalog("p", json.RawMessage(payload))
				if err != nil {
					t.Fatal(err)
				}
				requests := 0
				fetch := func(context.Context, *http.Request) (*http.Response, error) {
					requests++
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
				}
				wantAll := []string{"builtin-chat", "builtin-image", "builtin-classifier", "shared", "shared", "decision"}
				provider := WithRemoteCatalog(base, "https://catalog.invalid", 0, false, fetch)
				if personalized {
					provider = WithPersonalizedCatalog(base, func(models []*ai.Model, _ *ai.RefreshModelsContext) ([]*ai.Model, error) {
						if got := catalogModelIDs(models); !reflect.DeepEqual(got, wantAll) {
							t.Errorf("personalizer received %v, want %v", got, wantAll)
						}
						return models, nil
					}, "https://catalog.invalid", 0, false, fetch)
				}
				if err := provider.RefreshModels(&ai.RefreshModelsContext{
					Ctx: context.Background(), Stored: &ai.ModelsStoreEntry{Models: stored}, AllowNetwork: network,
					Publish: func(p ai.ModelsPublication) bool {
						if p.Update != nil {
							p.Update()
						}
						return true
					},
				}); err != nil {
					t.Fatal(err)
				}
				wantChat := []string{"builtin-chat", "shared"}
				wantImage := []string{"builtin-image", "shared"}
				wantClassifier := []string{"builtin-classifier", "decision"}
				if personalized && !network {
					wantAll = []string{"shared", "shared", "decision"}
					wantChat, wantImage, wantClassifier = []string{"shared"}, []string{"shared"}, []string{"decision"}
				}
				for name, models := range map[string][]*ai.Model{
					"all": provider.GetAllModels(), "chat": provider.GetModels(),
					"image": provider.GetModelsOfType("image"), "classifier": provider.GetClassifierModels(),
				} {
					want := map[string][]string{"all": wantAll, "chat": wantChat, "image": wantImage, "classifier": wantClassifier}[name]
					if got := catalogModelIDs(models); !reflect.DeepEqual(got, want) {
						t.Errorf("%s IDs = %v, want %v", name, got, want)
					}
				}
				wantRequests := 0
				if network {
					wantRequests = 1
				}
				if requests != wantRequests {
					t.Errorf("requests=%d, want %d", requests, wantRequests)
				}
			})
		}
	}
}
