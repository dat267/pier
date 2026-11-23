package mcp

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dat267/pier/mcp/oauth"
)

// AdaptOAuthProvider adapts an OAuthClientProvider to the streamable HTTP
// transport's AuthProvider (upstream adaptOAuthProvider). After a 401 it
// refreshes the tokens, or fails with McpOAuthAuthorizationRequiredError
// when the user has to authorize (again). Concurrent 401s share one
// refresh, and a request whose token was already replaced is just retried:
// with rotating refresh tokens, a second refresh with the old refresh token
// would fail and discard the new grant.
func AdaptOAuthProvider(provider oauth.OAuthClientProvider) AuthProvider {
	return &oauthAdapter{provider: provider}
}

type oauthAdapter struct {
	provider oauth.OAuthClientProvider

	mu       sync.Mutex
	inFlight *refreshRun
	// refreshAttempts counts proactive refreshes, so a test can tell the
	// pre-request path from the 401 path without an authorization server.
	refreshAttempts atomic.Int32
}

// RefreshAttempts reports how many proactive refreshes this adapter started.
func (a *oauthAdapter) RefreshAttempts() int32 { return a.refreshAttempts.Load() }

// mcpRefreshSkewMS refreshes an access token this close to expiring
// (auth-provider.ts REFRESH_SKEW_MS).
const mcpRefreshSkewMS = 30_000

// Token returns the current access token, refreshing it first when it has
// expired.
//
// The reference's token() does this proactively (`refreshStoredTokens` under the
// store's refresh lock), so an expired token is renewed before the request
// instead of being discovered by a 401. A refresh failure falls through and the
// request carries the old token, which the 401 path then handles; a redirect is
// swallowed for the same reason, because a refresh must never redirect the user.
func (a *oauthAdapter) Token(ctx context.Context) (string, error) {
	tokens, err := a.provider.Tokens(ctx)
	if err != nil || tokens == nil {
		return "", err
	}
	if !a.storedTokenExpired(ctx, tokens.AccessToken) {
		return tokens.AccessToken, nil
	}
	// The save is serialized by the provider's own load-modify-save (`update`
	// holds writesMu), so the refresh must not also hold it: AuthorizeMcp calls
	// SaveTokens inside the run and taking the lock here would block on itself.
	a.runRefreshLocked(ctx, tokens.AccessToken)
	current, err := a.provider.Tokens(ctx)
	if err != nil || current == nil {
		return tokens.AccessToken, nil
	}
	return current.AccessToken, nil
}

// storedTokenExpired reports whether the stored access token is within the
// refresh skew of its expiry. It re-reads the state because another request may
// have refreshed while this one waited.
func (a *oauthAdapter) storedTokenExpired(ctx context.Context, staleToken string) bool {
	expiresAt, accessToken, ok := tokenExpiry(ctx, a.provider)
	if !ok || expiresAt == nil || accessToken != staleToken {
		return false
	}
	return *expiresAt-mcpRefreshSkewMS <= time.Now().UnixMilli()
}

// tokenExpiry reads the stored expiry when the provider exposes it. The ported
// OAuthClientProvider surface does not (upstream reaches state.tokensExpireAt
// directly), so this is an optional accessor rather than a widened interface.
func tokenExpiry(ctx context.Context, provider oauth.OAuthClientProvider) (*int64, string, bool) {
	expirer, ok := provider.(interface {
		TokenExpiry(ctx context.Context) (*int64, string, bool)
	})
	if !ok {
		return nil, "", false
	}
	return expirer.TokenExpiry(ctx)
}

// runRefreshLocked refreshes once, sharing any run already in flight. A no-op
// when the stored token already moved on or no refresh token exists, which is
// the reference's check under the refresh lock.
func (a *oauthAdapter) runRefreshLocked(ctx context.Context, staleToken string) {
	a.mu.Lock()
	if a.inFlight != nil {
		run := a.inFlight
		a.mu.Unlock()
		run.wg.Wait()
		return
	}
	run := &refreshRun{}
	run.wg.Add(1)
	a.inFlight = run
	a.mu.Unlock()
	// Serialized by the provider's own load-modify-save: AuthorizeMcp calls
	// SaveTokens, which holds writesMu, so waiting here must not hold it.
	go func() {
		defer run.wg.Done()
		// The token may have moved while this waited for admission.
		expiresAt, accessToken, ok := tokenExpiry(ctx, a.provider)
		if !ok || accessToken != staleToken || expiresAt == nil {
			return
		}
		tokens, err := a.provider.Tokens(ctx)
		if err != nil || tokens == nil || tokens.RefreshToken == nil {
			return
		}
		// Failures and a redirect both fall through, as token() does: the
		// request goes out with the old token and a 401 decides what happens.
		a.refreshAttempts.Add(1)
		_, _ = oauth.AuthorizeMcp(ctx, a.provider, oauth.FlowOptions{RefreshOnly: true})
	}()
	run.wg.Wait()
	a.mu.Lock()
	a.inFlight = nil
	a.mu.Unlock()
}

// OnUnauthorized refreshes (or re-authorizes) once per run; concurrent
// callers share the in-flight run.
func (a *oauthAdapter) OnUnauthorized(ctx context.Context, info UnauthorizedInfo) error {
	challenge := oauth.ParseWwwAuthenticate(info.Response.Header.Get("www-authenticate"))
	insufficientScope := challenge.Error != nil && *challenge.Error == "insufficient_scope"

	a.mu.Lock()
	if !insufficientScope && a.inFlight == nil && info.Token != "" {
		// Tokens is user code and may re-enter this adapter. Recheck shared
		// refresh admission under the lock after the callback returns.
		a.mu.Unlock()
		current, err := a.provider.Tokens(ctx)
		if err == nil && current != nil && current.AccessToken != info.Token {
			// Another request already refreshed; the retried request
			// carries the new token.
			return nil
		}
		a.mu.Lock()
	}
	if a.inFlight == nil {
		run := &refreshRun{}
		run.wg.Add(1)
		a.inFlight = run
		a.mu.Unlock()
		go a.runRefresh(ctx, run, info, challenge, insufficientScope)
		run.wg.Wait()
		a.mu.Lock()
		a.inFlight = nil
		a.mu.Unlock()
		return run.err
	}
	run := a.inFlight
	a.mu.Unlock()
	run.wg.Wait()
	return run.err
}

// runRefresh performs the shared refresh or authorization.
func (a *oauthAdapter) runRefresh(ctx context.Context, run *refreshRun, info UnauthorizedInfo, challenge oauth.OAuthChallenge, insufficientScope bool) {
	defer run.wg.Done()
	var granted *oauth.OAuthTokens
	if insufficientScope {
		granted, _ = a.provider.Tokens(ctx)
	}
	var scope *string
	if insufficientScope {
		scope = oauth.StepUpScope(tokenScope(granted), challenge.Scope)
	} else {
		scope = challenge.Scope
	}
	var metadataURL *string
	if challenge.ResourceMetadataURL != nil {
		metadataURL = challenge.ResourceMetadataURL
	}
	result, err := oauth.AuthorizeMcp(ctx, a.provider, oauth.FlowOptions{
		ServerURL:           info.ServerURL,
		ResourceMetadataURL: metadataURL,
		Scope:               scope,
		Fetch:               wrapTransportFetch(info.Fetch),
		SkipRefresh:         insufficientScope,
	})
	if err == nil && result == oauth.ResultRedirect {
		err = &oauth.McpOAuthAuthorizationRequiredError{}
	}
	run.err = err
}

type refreshRun struct {
	wg  sync.WaitGroup
	err error
}

func tokenScope(tokens *oauth.OAuthTokens) *string {
	if tokens == nil {
		return nil
	}
	return tokens.Scope
}

// wrapTransportFetch converts the transport's fetch into the oauth fetch. Both
// have the same underlying type, so the conversion is enough; a nil fetch stays
// nil, which makes the oauth package use its default.
func wrapTransportFetch(fetch func(ctx context.Context, request *http.Request) (*http.Response, error)) oauth.McpFetch {
	if fetch == nil {
		return nil
	}
	return oauth.McpFetch(fetch)
}
