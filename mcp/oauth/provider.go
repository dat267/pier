package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// McpOAuthState is the persisted OAuth state for one server (upstream
// McpOAuthState).
type McpOAuthState struct {
	ServerURL         string
	ClientInformation *OAuthClientInformationFull
	Tokens            *OAuthTokens
	// TokensExpireAt is when the access token expires, in milliseconds
	// since the epoch, from `expires_in` at the time it was saved.
	TokensExpireAt *int64
	CodeVerifier   *string
	OAuthState     *string
	Discovery      *OAuthDiscoveryState
}

// McpOAuthStateStore persists the state (upstream McpOAuthStateStore).
type McpOAuthStateStore interface {
	Load(ctx context.Context) (*McpOAuthState, error)
	Save(ctx context.Context, state *McpOAuthState) error
}

// MemoryOAuthStateStore keeps one copy in memory (upstream
// MemoryOAuthStateStore).
type MemoryOAuthStateStore struct {
	mu    sync.Mutex
	value *McpOAuthState
}

// Load returns a deep copy of the stored state.
func (s *MemoryOAuthStateStore) Load(context.Context) (*McpOAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value == nil {
		return nil, nil
	}
	return cloneState(s.value), nil
}

// Save stores a deep copy.
func (s *MemoryOAuthStateStore) Save(_ context.Context, state *McpOAuthState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = cloneState(state)
	return nil
}

// cloneState deep-copies the state (upstream structuredClone).
func cloneState(state *McpOAuthState) *McpOAuthState {
	out := &McpOAuthState{ServerURL: state.ServerURL}
	if state.ClientInformation != nil {
		enc, _ := marshalJSON(state.ClientInformation)
		var client OAuthClientInformationFull
		_ = unmarshalJSON(enc, &client)
		out.ClientInformation = &client
	}
	if state.Tokens != nil {
		tokens := *state.Tokens
		if tokens.Scope != nil {
			scope := *tokens.Scope
			tokens.Scope = &scope
		}
		if tokens.RefreshToken != nil {
			refresh := *tokens.RefreshToken
			tokens.RefreshToken = &refresh
		}
		if tokens.IDToken != nil {
			id := *tokens.IDToken
			tokens.IDToken = &id
		}
		if tokens.ExpiresIn != nil {
			expires := *tokens.ExpiresIn
			tokens.ExpiresIn = &expires
		}
		out.Tokens = &tokens
	}
	if state.TokensExpireAt != nil {
		expire := *state.TokensExpireAt
		out.TokensExpireAt = &expire
	}
	if state.CodeVerifier != nil {
		verifier := *state.CodeVerifier
		out.CodeVerifier = &verifier
	}
	if state.OAuthState != nil {
		oauthState := *state.OAuthState
		out.OAuthState = &oauthState
	}
	if state.Discovery != nil {
		discovery := *state.Discovery
		if discovery.ResourceMetadataURL != nil {
			resourceURL := *discovery.ResourceMetadataURL
			discovery.ResourceMetadataURL = &resourceURL
		}
		out.Discovery = &discovery
	}
	return out
}

// McpOAuthProviderOptions configure the default provider (upstream
// McpOAuthProviderOptions).
type McpOAuthProviderOptions struct {
	ServerURL      string
	RedirectURL    string
	ClientMetadata OAuthClientMetadata
	ClientID       string
	ClientSecret   string
	Store          McpOAuthStateStore
	OnRedirect     func(ctx context.Context, url string) error
}

// McpOAuthProvider is the default stateful provider for one exact MCP server
// URL; applications inject durable storage if needed (upstream
// McpOAuthProvider).
type McpOAuthProvider struct {
	// redirectURL is the loopback redirect (upstream's redirectUrl
	// property; a Go field cannot share the interface method's name).
	redirectURL string
	// Metadata is the effective RFC 7591 client metadata with the defaults
	// applied (upstream's clientMetadata property).
	Metadata         OAuthClientMetadata
	serverURL        string
	configuredClient *OAuthClientInformation
	store            McpOAuthStateStore
	onRedirect       func(ctx context.Context, url string) error

	// writes serializes state updates against loads, mirroring upstream's
	// promise chain.
	writesMu sync.Mutex
}

// NewMcpOAuthProvider builds the provider; the client metadata gets the
// RFC 7591 defaults (redirect_uris, grant_types, response_types, and the
// auth method from the secret's presence).
func NewMcpOAuthProvider(options McpOAuthProviderOptions) *McpOAuthProvider {
	metadata := options.ClientMetadata
	if len(metadata.RedirectURIs) == 0 {
		metadata.RedirectURIs = []string{options.RedirectURL}
	}
	if metadata.GrantTypes == nil {
		metadata.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if metadata.ResponseTypes == nil {
		metadata.ResponseTypes = []string{"code"}
	}
	if metadata.TokenEndpointAuthMethod == nil {
		method := "none"
		if options.ClientSecret != "" {
			method = "client_secret_post"
		}
		metadata.TokenEndpointAuthMethod = &method
	}
	store := options.Store
	if store == nil {
		store = &MemoryOAuthStateStore{}
	}
	var configuredClient *OAuthClientInformation
	if options.ClientID != "" {
		configuredClient = &OAuthClientInformation{ClientID: options.ClientID}
		if options.ClientSecret != "" {
			secret := options.ClientSecret
			configuredClient.ClientSecret = &secret
		}
	}
	return &McpOAuthProvider{
		redirectURL:      options.RedirectURL,
		Metadata:         metadata,
		serverURL:        options.ServerURL,
		configuredClient: configuredClient,
		store:            store,
		onRedirect:       options.OnRedirect,
	}
}

// RedirectURL returns the loopback redirect (upstream's redirectUrl
// property).
func (p *McpOAuthProvider) RedirectURL() string { return p.redirectURL }

// AddClientAuthentication returns nil: the provider applies the default
// auth selection (upstream McpOAuthProvider has no override).
func (p *McpOAuthProvider) AddClientAuthentication() AddClientAuthentication { return nil }

// ClientMetadata returns the effective client metadata (upstream's
// clientMetadata property).
func (p *McpOAuthProvider) ClientMetadata() OAuthClientMetadata { return p.Metadata }

// ClientMetadataURL is unused by the default provider.
func (p *McpOAuthProvider) ClientMetadataURL() *string { return nil }

// State returns the pending oauth state parameter, generating it on first
// use (upstream state).
func (p *McpOAuthProvider) State(ctx context.Context) (string, error) {
	state, err := p.load(ctx)
	if err != nil {
		return "", err
	}
	if state.OAuthState != nil {
		return *state.OAuthState, nil
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	value := hex.EncodeToString(bytes)
	if err := p.update(ctx, func(next *McpOAuthState) { next.OAuthState = &value }); err != nil {
		return "", err
	}
	return value, nil
}

// ClientInformation returns the configured or registered client (upstream
// clientInformation).
func (p *McpOAuthProvider) ClientInformation(ctx context.Context) (*OAuthClientInformation, error) {
	if p.configuredClient != nil {
		return p.configuredClient, nil
	}
	_ = true
	state, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	if state.ClientInformation == nil {
		return nil, nil
	}
	return &state.ClientInformation.OAuthClientInformation, nil
}

// SaveClientInformation stores a dynamic registration (upstream
// saveClientInformation); the configured client is never overwritten.
func (p *McpOAuthProvider) SaveClientInformation(ctx context.Context, information *OAuthClientInformationFull) error {
	if p.configuredClient != nil {
		return nil
	}
	return p.update(ctx, func(next *McpOAuthState) { next.ClientInformation = information })
}

// Tokens returns the stored tokens (upstream tokens).
func (p *McpOAuthProvider) Tokens(ctx context.Context) (*OAuthTokens, error) {
	state, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	return state.Tokens, nil
}

// SaveTokens stores tokens and derives the expiry (upstream saveTokens).
func (p *McpOAuthProvider) SaveTokens(ctx context.Context, tokens *OAuthTokens) error {
	return p.update(ctx, func(next *McpOAuthState) {
		next.Tokens = tokens
		if tokens.ExpiresIn == nil {
			next.TokensExpireAt = nil
		} else {
			expire := time.Now().UnixMilli() + *tokens.ExpiresIn*1000
			next.TokensExpireAt = &expire
		}
	})
}

// RedirectToAuthorization hands the authorization URL to the application
// (upstream redirectToAuthorization).
func (p *McpOAuthProvider) RedirectToAuthorization(ctx context.Context, url string) error {
	return p.onRedirect(ctx, url)
}

// SaveCodeVerifier stores the PKCE verifier (upstream saveCodeVerifier).
func (p *McpOAuthProvider) SaveCodeVerifier(ctx context.Context, verifier string) error {
	return p.update(ctx, func(next *McpOAuthState) { next.CodeVerifier = &verifier })
}

// CodeVerifier returns the stored verifier (upstream codeVerifier).
func (p *McpOAuthProvider) CodeVerifier(ctx context.Context) (string, error) {
	state, err := p.load(ctx)
	if err != nil {
		return "", err
	}
	if state.CodeVerifier == nil {
		return "", errors.New("No OAuth PKCE code verifier is stored")
	}
	return *state.CodeVerifier, nil
}

// InvalidateCredentials clears the named credentials (upstream
// invalidateCredentials).
func (p *McpOAuthProvider) InvalidateCredentials(ctx context.Context, kind string) error {
	return p.update(ctx, func(next *McpOAuthState) {
		if kind == "all" || kind == "client" {
			next.ClientInformation = nil
		}
		if kind == "all" || kind == "tokens" {
			next.Tokens = nil
			next.TokensExpireAt = nil
		}
		if kind == "all" || kind == "verifier" {
			next.CodeVerifier = nil
		}
		if kind == "all" || kind == "discovery" {
			next.Discovery = nil
		}
		if kind == "all" {
			next.OAuthState = nil
		}
	})
}

// SaveDiscoveryState caches discovery (upstream saveDiscoveryState).
func (p *McpOAuthProvider) SaveDiscoveryState(ctx context.Context, discovery *OAuthDiscoveryState) error {
	return p.update(ctx, func(next *McpOAuthState) { next.Discovery = discovery })
}

// DiscoveryState returns the cached discovery (upstream discoveryState).
func (p *McpOAuthProvider) DiscoveryState(ctx context.Context) (*OAuthDiscoveryState, error) {
	state, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	return state.Discovery, nil
}

func (p *McpOAuthProvider) load(ctx context.Context) (*McpOAuthState, error) {
	p.writesMu.Lock()
	defer p.writesMu.Unlock()
	state, err := p.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return p.own(state), nil
}

// update serializes a load-modify-save against concurrent writers.
func (p *McpOAuthProvider) update(ctx context.Context, mutate func(*McpOAuthState)) error {
	p.writesMu.Lock()
	defer p.writesMu.Unlock()
	state, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	next := p.own(state)
	mutate(next)
	return p.store.Save(ctx, next)
}

// own ignores stored state for another server URL so credentials never leak
// across servers.
func (p *McpOAuthProvider) own(state *McpOAuthState) *McpOAuthState {
	if state != nil && state.ServerURL == p.serverURL {
		return state
	}
	return &McpOAuthState{ServerURL: p.serverURL}
}
