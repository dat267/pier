package coding

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// repeatByte serves count copies of one byte without allocating the body.
type repeatByte struct {
	value     byte
	remaining int64
}

func (r *repeatByte) Read(buffer []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	length := int64(len(buffer))
	if length > r.remaining {
		length = r.remaining
	}
	for index := int64(0); index < length; index++ {
		buffer[index] = r.value
	}
	r.remaining -= length
	return int(length), nil
}

// D341: the pi.dev catalog is untrusted network input that the reference hands
// whole to response.json() (core/remote-catalog-provider.ts readCatalog), so the
// port bounds it like the other provider documents. A body over the limit fails
// as a protocol error instead of being truncated into a JSON syntax error.
func TestRemoteCatalogRejectsOversizedBody(t *testing.T) {
	fetch := func(context.Context, *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&repeatByte{value: '[', remaining: remoteCatalogBodyLimit + 1}),
		}, nil
	}
	wrapped := WithRemoteCatalog(catalogProvider(t), "https://catalog.example", 0, false, fetch)
	err := wrapped.RefreshModels(&ai.RefreshModelsContext{
		AllowNetwork: true, Ctx: context.Background(),
		Publish: func(ai.ModelsPublication) bool { return true },
	})
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized catalog = %v, want a size limit error", err)
	}
}

// The same path still accepts a catalog inside the limit.
func TestRemoteCatalogAcceptsBodyInsideTheLimit(t *testing.T) {
	fetch := func(context.Context, *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[{"id":"remote-model","type":"chat"}]`)),
		}, nil
	}
	wrapped := WithRemoteCatalog(catalogProvider(t), "https://catalog.example", 0, false, fetch)
	if err := wrapped.RefreshModels(&ai.RefreshModelsContext{
		AllowNetwork: true, Ctx: context.Background(),
		Publish: func(publication ai.ModelsPublication) bool {
			if publication.Update != nil {
				publication.Update()
			}
			return true
		},
	}); err != nil {
		t.Fatalf("catalog refresh: %v", err)
	}
	found := false
	for _, model := range wrapped.GetModels() {
		if model.ID == "remote-model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("remote model missing from %v", wrapped.GetModels())
	}
}
