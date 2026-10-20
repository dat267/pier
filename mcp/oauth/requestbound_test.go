package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A discovery fetch must not sit on a half-open endpoint: the login cannot
// depend on the platform client having a timeout (D255).
func TestDefaultFetchIsBounded(t *testing.T) {
	restore := defaultRequestTimeout
	defaultRequestTimeout = 100 * time.Millisecond
	defer func() { defaultRequestTimeout = restore }()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, ferr := defaultFetch(context.Background(), request)
		if response != nil {
			response.Body.Close()
		}
		done <- ferr
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled discovery fetch returned no error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stalled discovery fetch did not settle; the request is unbounded")
	}
}
