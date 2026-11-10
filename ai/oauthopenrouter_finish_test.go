package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A completed callback answers the browser and settles the wait at once.
// Upstream auth/oauth/callback-server.ts (pi v1.5.0 f9a26365) finish only
// settles; the server closes later from the login's finally. Shutting the
// server down inside the handler made Shutdown wait out its whole timeout for
// the handler's own connection, delaying the page and the credential by 1s.
func TestOpenRouterCallbackSettlesWithoutWaitingForShutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"key":"sk-fast"}`))
	}))
	defer server.Close()
	withOpenRouterTokenURL(t, server.URL, func() {
		callback, err := StartOpenRouterCallbackServer("/oauth/callback/fast", "verifier", context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer callback.Close()
		start := time.Now()
		response, err := http.Get(callback.CallbackURL + "?code=abc")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		credential, err := callback.WaitForCredential()
		if err != nil || credential == nil || credential.Access != "sk-fast" {
			t.Fatalf("credential = %+v, err = %v", credential, err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("callback took %v to answer and settle, want well under the 1s shutdown timeout", elapsed)
		}
	})
}
