package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type stalledDiscoveryBody struct {
	ctx    context.Context
	closed bool
}

func (b *stalledDiscoveryBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledDiscoveryBody) Close() error {
	b.closed = true
	return nil
}

// pi v1.4.0 (4a080252), packages/mcp/src/oauth/discovery.ts discard()
// cancels a missing document's body without reading it before the fallback.
func TestDiscoveryDoesNotDrainStalledMissingDocument(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		attempts := 0
		missing := &stalledDiscoveryBody{ctx: ctx}
		fetch := func(ctx context.Context, request *http.Request) (*http.Response, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			attempts++
			if attempts == 1 {
				return &http.Response{StatusCode: http.StatusNotFound, Body: missing}, nil
			}
			if request.URL.Path != "/.well-known/oauth-protected-resource" {
				t.Errorf("fallback path = %q", request.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"resource":"https://fixture.invalid/mcp"}`)),
			}, nil
		}
		metadata, err := DiscoverProtectedResourceMetadata(ctx, "https://fixture.invalid/mcp", DiscoveryOptions{Fetch: fetch})
		if err != nil {
			t.Fatalf("discovery waited for missing document body: %v (attempts=%d)", err, attempts)
		}
		if metadata == nil || metadata.Resource != "https://fixture.invalid/mcp" || attempts != 2 {
			t.Fatalf("metadata=%+v attempts=%d", metadata, attempts)
		}
		if !missing.closed {
			t.Fatal("missing document body not closed")
		}
	})
}
