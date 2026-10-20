package ai

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Non-streaming provider, auth and config requests are bounded like upstream's
// dispatcher bounds every fetch (D255): a half-open endpoint must fail the call,
// and a caller deadline always wins over the default.

func TestBoundedRequestHonorsTheCallerDeadline(t *testing.T) {
	restore := defaultBoundedRequestTimeout
	defaultBoundedRequestTimeout = 5 * time.Second
	defer func() { defaultBoundedRequestTimeout = restore }()

	server := stallServer(t, false, "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := doBoundedRequest(req); err == nil {
		t.Fatal("a caller-deadline request against a stalled endpoint returned no error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("caller deadline took %v; want under 2s", elapsed)
	}
}

func TestGitHubCopilotTokenRefreshIsBounded(t *testing.T) {
	restoreTimeout := defaultBoundedRequestTimeout
	defaultBoundedRequestTimeout = 100 * time.Millisecond
	defer func() { defaultBoundedRequestTimeout = restoreTimeout }()

	server := stallServer(t, false, "")
	restoreURLs := githubCopilotURLsBuilder
	githubCopilotURLsBuilder = func(domain string) GitHubCopilotURLs {
		return GitHubCopilotURLs{CopilotTokenURL: server.URL}
	}
	defer func() { githubCopilotURLsBuilder = restoreURLs }()

	done := make(chan error, 1)
	go func() {
		_, err := RefreshGitHubCopilotAccessToken(context.Background(), "token", "")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled token refresh returned no error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stalled token refresh did not settle; the request is unbounded")
	}
}
