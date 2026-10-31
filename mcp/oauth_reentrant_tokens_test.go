package mcp

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/oauth"
)

type callbackTokensProvider struct {
	oauth.OAuthClientProvider
	readTokens func(context.Context) (*oauth.OAuthTokens, error)
}

func (p *callbackTokensProvider) Tokens(ctx context.Context) (*oauth.OAuthTokens, error) {
	return p.readTokens(ctx)
}

// pi 4a080252 packages/mcp/src/oauth/flow.ts adaptOAuthProvider awaits the
// user provider's tokens callback without retaining a lock. A token-store hook
// may re-enter the adapter, including a concurrent stale-token notification.
func TestOAuthAdapterTokenCallbackCanReenter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	info := UnauthorizedInfo{
		Response:  &http.Response{Header: http.Header{}},
		ServerURL: "https://example.invalid/mcp", Token: "stale",
		Fetch: func(context.Context, *http.Request) (*http.Response, error) {
			return nil, errors.New("unexpected fetch during stale-token check")
		},
	}
	var entered, stalled atomic.Bool
	nestedDone := make(chan struct{})
	nestedErr := make(chan error, 1)
	provider := &callbackTokensProvider{OAuthClientProvider: newTestProvider("http://127.0.0.1/callback")}
	adapter := AdaptOAuthProvider(provider)
	provider.readTokens = func(ctx context.Context) (*oauth.OAuthTokens, error) {
		if entered.CompareAndSwap(false, true) {
			go func() {
				nestedErr <- adapter.OnUnauthorized(ctx, info)
				close(nestedDone)
			}()
			select {
			case <-nestedDone:
			case <-time.After(time.Second):
				// Release the outer callback on the red path so the blocked
				// notification can finish instead of leaking a goroutine.
				stalled.Store(true)
			}
		}
		return &oauth.OAuthTokens{AccessToken: "fresh"}, nil
	}
	if err := adapter.OnUnauthorized(ctx, info); err != nil {
		t.Fatal(err)
	}
	select {
	case <-nestedDone:
	case <-ctx.Done():
		t.Fatal("nested notification did not finish:", ctx.Err())
	}
	if err := <-nestedErr; err != nil {
		t.Fatal(err)
	}
	if stalled.Load() {
		t.Fatal("token callback blocked reentrant OnUnauthorized until the callback returned")
	}
}
