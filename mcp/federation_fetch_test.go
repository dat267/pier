package mcp

import (
	"context"
	"net/http"
	"testing"
)

// wrapTransportFetch keeps a transport's fetch (the OAuth flow must reuse the
// transport's HTTP behaviour) and leaves a nil one nil so the oauth package
// installs its default.
func TestWrapTransportFetch(t *testing.T) {
	if got := wrapTransportFetch(nil); got != nil {
		t.Fatalf("nil fetch became %v", got)
	}
	called := false
	original := func(context.Context, *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK}, nil
	}
	wrapped := wrapTransportFetch(original)
	if wrapped == nil {
		t.Fatal("a transport fetch was dropped")
	}
	if _, err := wrapped(context.Background(), &http.Request{}); err != nil || !called {
		t.Fatalf("wrapped fetch = %v, called %v", err, called)
	}
}
