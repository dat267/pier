package mcp

import (
	"context"
	"net/http"
	"sync"

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
}

// Token returns the current access token, if any.
func (a *oauthAdapter) Token(ctx context.Context) (string, error) {
	tokens, err := a.provider.Tokens(ctx)
	if err != nil || tokens == nil {
		return "", err
	}
	return tokens.AccessToken, nil
}

// OnUnauthorized refreshes (or re-authorizes) once per run; concurrent
// callers share the in-flight run.
func (a *oauthAdapter) OnUnauthorized(ctx context.Context, info UnauthorizedInfo) error {
	challenge := oauth.ParseWwwAuthenticate(info.Response.Header.Get("www-authenticate"))
	insufficientScope := challenge.Error != nil && *challenge.Error == "insufficient_scope"

	a.mu.Lock()
	if !insufficientScope && a.inFlight == nil && info.Token != "" {
		current, err := a.provider.Tokens(ctx)
		if err == nil && current != nil && current.AccessToken != info.Token {
			// Another request already refreshed; the retried request
			// carries the new token.
			a.mu.Unlock()
			return nil
		}
	}
	if a.inFlight == nil {
		run := &refreshRun{}
		run.wg.Add(1)
		a.inFlight = run
		a.mu.Unlock()
		go a.runRefresh(run, ctx, info, challenge, insufficientScope)
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
func (a *oauthAdapter) runRefresh(run *refreshRun, ctx context.Context, info UnauthorizedInfo, challenge oauth.OAuthChallenge, insufficientScope bool) {
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

// wrapTransportFetch converts the transport's fetch into the oauth fetch.
func wrapTransportFetch(fetch func(ctx context.Context, request *http.Request) (*http.Response, error)) oauth.McpFetch {
	if fetch == nil {
		return nil
	}
	return func(ctx context.Context, request *http.Request) (*http.Response, error) {
		return fetch(ctx, request)
	}
}
