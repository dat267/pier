package coding

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 remote-catalog-provider.ts evaluates Date.now() - checkedAt with
// JavaScript Number arithmetic; an int64 underflow must not make stale data fresh.
func TestRemoteCatalogMinimumCheckedAtTriggersRefresh(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	checkedAt, lastModified := int64(math.MinInt64), int64(1)
	stored := &ai.ModelsStoreEntry{
		Models:       []*ai.Model{{ID: "cached", Provider: "anthropic"}},
		CheckedAt:    &checkedAt,
		LastModified: &lastModified,
	}
	provider := WithRemoteCatalog(catalogProvider(t), server.URL, 0, false, nil)
	if err := provider.RefreshModels(&ai.RefreshModelsContext{
		AllowNetwork: true,
		Ctx:          context.Background(),
		Stored:       stored,
		Publish:      func(ai.ModelsPublication) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one revalidation for stale minimum checkedAt", requests)
	}
}
