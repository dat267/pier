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

// expiryTokensProvider reports a stored token expiry, which only the concrete
// provider exposes (the ported OAuthClientProvider surface does not).
type expiryTokensProvider struct {
	*callbackTokensProvider
	tokens     *oauth.OAuthTokens
	expireAt   int64
	refreshRan atomic.Int32
}

func (p *expiryTokensProvider) Tokens(context.Context) (*oauth.OAuthTokens, error) {
	return p.tokens, nil
}

// TokenExpiry implements the optional accessor the adapter type-asserts.
func (p *expiryTokensProvider) TokenExpiry(context.Context) (*int64, string, bool) {
	expire := p.expireAt
	return &expire, p.tokens.AccessToken, true
}

// SaveTokens records a refresh without needing an authorization server.
func (p *expiryTokensProvider) SaveTokens(_ context.Context, tokens *oauth.OAuthTokens) error {
	p.refreshRan.Add(1)
	p.tokens = tokens
	return nil
}

// TestOAuthAdapterTokenRefreshesAnExpiredTokenProactively covers the reference's
// token(): an access token inside REFRESH_SKEW_MS of its expiry is refreshed
// before the request, so the 401 path is not what discovers it. A refresh that
// cannot run leaves the old token, which the 401 then handles.
func TestOAuthAdapterTokenRefreshesAnExpiredTokenProactively(t *testing.T) {
	base := newTestProvider("http://127.0.0.1/callback")
	for _, testCase := range []struct {
		name     string
		expireAt int64
		refresh  bool
		// attempts is whether the adapter tried to refresh: the fixture cannot
		// reach an authorization server, so a success would prove nothing about
		// the wiring, and the old token coming back is the fall-through.
		attempts int32
	}{
		{"expired with a refresh token", time.Now().UnixMilli() + 1_000, true, 1},
		{"well inside its lifetime", time.Now().UnixMilli() + 10*60_000, false, 0},
		{"expired without a refresh token", time.Now().UnixMilli() + 1_000, false, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens := &oauth.OAuthTokens{AccessToken: "stale"}
			if testCase.refresh {
				tokens.RefreshToken = new("refresh")
			}
			provider := &expiryTokensProvider{
				callbackTokensProvider: &callbackTokensProvider{OAuthClientProvider: base},
				tokens:                 tokens,
				expireAt:               testCase.expireAt,
			}
			adapter := AdaptOAuthProvider(provider)
			// A refresh that reaches the network cannot in this fixture, so a
			// refresh attempt leaves the token alone; the point is whether the
			// adapter tried, and that the old token still comes back.
			got, err := adapter.Token(context.Background())
			if err != nil {
				t.Fatalf("Token: %v", err)
			}
			if got != "stale" {
				t.Fatalf("Token = %q, want the old token to fall through", got)
			}
			if attempts := adapter.(*oauthAdapter).RefreshAttempts(); int32(attempts) != testCase.attempts {
				t.Fatalf("refresh attempts = %d, want %d", attempts, testCase.attempts)
			}
		})
	}
}
