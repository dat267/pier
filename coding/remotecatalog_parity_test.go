package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts withRemoteCatalog:
// 404/501 retain stored.models while invalidating Last-Modified and ETag.
func TestRemoteCatalogMissingRetainsCachedBody(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusNotImplemented} {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/cached=%t", status, cached), func(t *testing.T) {
				ctx := context.Background()
				var stored *ai.ModelsStoreEntry
				if cached {
					modified, etag := int64(100), `"old"`
					stored = &ai.ModelsStoreEntry{Models: []*ai.Model{{ID: "cached", Provider: "p"}}, LastModified: &modified, Etag: &etag}
				}
				requests := 0
				fetch := func(context.Context, *http.Request) (*http.Response, error) {
					requests++
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				path := filepath.Join(t.TempDir(), "models-store.json")
				store := NewFileModelsStore(path)
				base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p", Models: []*ai.Model{{ID: "builtin", Provider: "p"}}})
				provider := WithRemoteCatalog(base, "https://catalog.invalid", 0, false, fetch)
				if err := provider.RefreshModels(&ai.RefreshModelsContext{
					Ctx: ctx, Stored: stored, AllowNetwork: true, Force: boolPtr(true),
					Publish: func(p ai.ModelsPublication) bool {
						if p.Update != nil {
							p.Update()
						}
						if p.Persist != nil {
							if err := store.Write("p", p.Persist, ctx); err != nil {
								t.Fatal(err)
							}
						}
						return true
					},
				}); err != nil {
					t.Fatal(err)
				}
				saved, err := store.Read("p", ctx)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{}
				if cached {
					want = append(want, "cached")
				}
				if got := catalogModelIDs(saved.Models); !reflect.DeepEqual(got, want) {
					t.Fatalf("persisted IDs = %v, want %v", got, want)
				}
				if saved.LastModified == nil || *saved.LastModified != 0 || saved.Etag != nil || saved.CheckedAt == nil {
					t.Fatalf("metadata = %+v", saved)
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var disk map[string]struct {
					Models json.RawMessage `json:"models"`
				}
				if err := json.Unmarshal(raw, &disk); err != nil {
					t.Fatal(err)
				}
				if !cached && string(disk["p"].Models) != "[]" {
					t.Fatalf("empty models = %s, want []", disk["p"].Models)
				}
				// A fresh wrapper restores the body offline only without a newer builtin catalog.
				for _, hasGeneratedAt := range []bool{false, true} {
					reloaded := WithRemoteCatalog(base, "https://catalog.invalid", 1, hasGeneratedAt, fetch)
					if err := reloaded.RefreshModels(&ai.RefreshModelsContext{Ctx: ctx, Stored: saved, Publish: func(p ai.ModelsPublication) bool {
						if p.Update != nil {
							p.Update()
						}
						return true
					}}); err != nil {
						t.Fatal(err)
					}
					wantLive := []string{"builtin"}
					if cached && !hasGeneratedAt {
						wantLive = append(wantLive, "cached")
					}
					if got := catalogModelIDs(reloaded.GetModels()); !reflect.DeepEqual(got, wantLive) {
						t.Fatalf("offline IDs (generatedAt=%t) = %v, want %v", hasGeneratedAt, got, wantLive)
					}
				}
				if requests != 1 {
					t.Fatalf("requests = %d, want 1", requests)
				}
			})
		}
	}
}

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts isSupportedModelType.
func TestParseRemoteCatalogSupportedTypes(t *testing.T) {
	models, err := ParseRemoteCatalog("p", []any{
		map[string]any{"id": "legacy"},
		map[string]any{"id": "chat", "type": "chat"},
		map[string]any{"id": "image", "type": "image"},
		map[string]any{"id": "classifier", "type": "classifier"},
		map[string]any{"id": "future", "type": "embedding"},
		map[string]any{"id": "empty", "type": ""},
		map[string]any{"id": "null", "type": nil},
		map[string]any{"id": "number", "type": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalogModelIDs(models), []string{"legacy", "chat", "image", "classifier"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts: both wrappers
// retain non-chat models in storage, but getModels only exposes chat models.
func TestCatalogGetModelsExcludesNonChat(t *testing.T) {
	for _, personalized := range []bool{false, true} {
		t.Run(fmt.Sprintf("personalized=%t", personalized), func(t *testing.T) {
			requests := 0
			fetch := func(context.Context, *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"id":"legacy"},{"id":"chat","type":"chat"},{"id":"picture","type":"image"},{"id":"decision","type":"classifier"},{"id":"future","type":"embedding"}]`))}, nil
			}
			base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p", Models: []*ai.Model{{ID: "builtin", Provider: "p"}}})
			wrap := func() *ai.Provider {
				if personalized {
					return WithPersonalizedCatalog(base, func(models []*ai.Model, _ *ai.RefreshModelsContext) ([]*ai.Model, error) { return models, nil }, "https://catalog.invalid", 0, false, fetch)
				}
				return WithRemoteCatalog(base, "https://catalog.invalid", 0, false, fetch)
			}
			provider := wrap()
			var saved *ai.ModelsStoreEntry
			if err := provider.RefreshModels(&ai.RefreshModelsContext{Ctx: context.Background(), AllowNetwork: true, Publish: func(p ai.ModelsPublication) bool {
				if p.Update != nil {
					p.Update()
				}
				if p.Persist != nil {
					saved = p.Persist
				}
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			want := []string{"builtin", "legacy", "chat"}
			if got := catalogModelIDs(provider.GetModels()); !reflect.DeepEqual(got, want) {
				t.Fatalf("live chat IDs = %v, want %v", got, want)
			}
			wantSaved := []string{"legacy", "chat", "picture", "decision"}
			if personalized {
				wantSaved = append([]string{"builtin"}, wantSaved...)
			}
			if got := catalogModelIDs(saved.Models); !reflect.DeepEqual(got, wantSaved) {
				t.Fatalf("persisted IDs = %v, want %v", got, wantSaved)
			}
			// A shared models-store.json may contain non-chat models written by pi.
			saved.Models = append(saved.Models, &ai.Model{ID: "foreign", Provider: "other"})
			offline := wrap()
			if err := offline.RefreshModels(&ai.RefreshModelsContext{Ctx: context.Background(), Stored: saved, Publish: func(p ai.ModelsPublication) bool {
				if p.Update != nil {
					p.Update()
				}
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			if got := catalogModelIDs(offline.GetModels()); !reflect.DeepEqual(got, want) {
				t.Fatalf("offline chat IDs = %v, want %v", got, want)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts requestCatalog.
func TestCatalogRequestsSupportedModelTypes(t *testing.T) {
	for _, personalized := range []bool{false, true} {
		t.Run(fmt.Sprintf("personalized=%t", personalized), func(t *testing.T) {
			var types string
			fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
				types = request.URL.Query().Get("types")
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("[]"))}, nil
			}
			base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p"})
			provider := WithRemoteCatalog(base, "https://catalog.invalid", 0, false, fetch)
			if personalized {
				provider = WithPersonalizedCatalog(base, func(models []*ai.Model, _ *ai.RefreshModelsContext) ([]*ai.Model, error) { return models, nil }, "https://catalog.invalid", 0, false, fetch)
			}
			if err := provider.RefreshModels(&ai.RefreshModelsContext{Ctx: context.Background(), AllowNetwork: true, Publish: func(p ai.ModelsPublication) bool {
				if p.Update != nil {
					p.Update()
				}
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			if types != "chat,image,classifier" {
				t.Fatalf("types query = %q, want chat,image,classifier", types)
			}
		})
	}
}

// Source-generated pi v1.4.0 (4a080252) ordering fixtures. Regenerate:
// node coding/testdata/remote-catalog/generate.mjs ./pi > coding/testdata/remote-catalog/golden.json
func TestRemoteCatalogOrderMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/remote-catalog/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name         string   `json:"name"`
		Payload      string   `json:"payload"`
		Personalized bool     `json:"personalized"`
		PersistedIDs []string `json:"persistedIds"`
		ChatIDs      []string `json:"chatIds"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%s/personalized=%t", fixture.Name, fixture.Personalized), func(t *testing.T) {
			fetch := func(context.Context, *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fixture.Payload))}, nil
			}
			base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p"})
			provider := WithRemoteCatalog(base, "https://catalog.invalid", 0, false, fetch)
			if fixture.Personalized {
				provider = WithPersonalizedCatalog(base, func(models []*ai.Model, _ *ai.RefreshModelsContext) ([]*ai.Model, error) { return models, nil }, "https://catalog.invalid", 0, false, fetch)
			}
			ctx := context.Background()
			store := NewFileModelsStore(filepath.Join(t.TempDir(), "models-store.json"))
			if err := provider.RefreshModels(&ai.RefreshModelsContext{Ctx: ctx, AllowNetwork: true, Publish: func(p ai.ModelsPublication) bool {
				if p.Update != nil {
					p.Update()
				}
				if p.Persist != nil {
					if err := store.Write("p", p.Persist, ctx); err != nil {
						t.Fatal(err)
					}
				}
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			if got := catalogModelIDs(provider.GetModels()); !reflect.DeepEqual(got, fixture.ChatIDs) {
				t.Errorf("published IDs = %v, want %v", got, fixture.ChatIDs)
			}
			saved, err := store.Read("p", ctx)
			if err != nil {
				t.Fatal(err)
			}
			if saved == nil {
				t.Fatal("catalog was not persisted")
			}
			if got := catalogModelIDs(saved.Models); !reflect.DeepEqual(got, fixture.PersistedIDs) {
				t.Errorf("persisted IDs = %v, want %v", got, fixture.PersistedIDs)
			}
		})
	}
}

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts requestCatalog uses
// new URL('/api/models/providers/' + encodeURIComponent(id), catalogBaseUrl).
func TestCatalogRequestUsesBaseOrigin(t *testing.T) {
	for _, test := range []struct{ name, baseURL, providerID, wantURL string }{
		{"path", "https://catalog.invalid/mirror", "p", "https://catalog.invalid/api/models/providers/p?types=chat%2Cimage%2Cclassifier"},
		{"query-fragment", "https://catalog.invalid/mirror?token=base#fragment", "p", "https://catalog.invalid/api/models/providers/p?types=chat%2Cimage%2Cclassifier"},
		{"provider-encoding", "https://catalog.invalid/mirror/", "p/a+ b:!'()*~", "https://catalog.invalid/api/models/providers/p%2Fa%2B%20b%3A!'()*~?types=chat%2Cimage%2Cclassifier"},
	} {
		for _, personalized := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/personalized=%t", test.name, personalized), func(t *testing.T) {
				var gotURL string
				fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
					gotURL = request.URL.String()
					status, payload := http.StatusNotFound, ""
					if gotURL == test.wantURL {
						status, payload = http.StatusOK, `[{"id":"remote"}]`
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
				}
				base := ai.CreateProvider(ai.CreateProviderOptions{ID: test.providerID})
				provider := WithRemoteCatalog(base, test.baseURL, 0, false, fetch)
				if personalized {
					provider = WithPersonalizedCatalog(base, func(models []*ai.Model, _ *ai.RefreshModelsContext) ([]*ai.Model, error) { return models, nil }, test.baseURL, 0, false, fetch)
				}
				if err := provider.RefreshModels(&ai.RefreshModelsContext{Ctx: context.Background(), AllowNetwork: true, Publish: func(p ai.ModelsPublication) bool {
					if p.Update != nil {
						p.Update()
					}
					return true
				}}); err != nil {
					t.Fatal(err)
				}
				if gotURL != test.wantURL {
					t.Errorf("URL = %q, want %q", gotURL, test.wantURL)
				}
				if got := catalogModelIDs(provider.GetModels()); !reflect.DeepEqual(got, []string{"remote"}) {
					t.Errorf("published IDs = %v, want [remote]", got)
				}
			})
		}
	}
}

func catalogModelIDs(models []*ai.Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
