package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client auth methods (upstream ClientAuthMethod).
const (
	ClientAuthSecretBasic = "client_secret_basic"
	ClientAuthSecretPost  = "client_secret_post"
	ClientAuthNone        = "none"
)

// AddClientAuthentication customizes the token-request auth (upstream
// AddClientAuthentication).
type AddClientAuthentication func(headers map[string]string, params url.Values, tokenURL string, metadata *AuthorizationServerMetadata) error

// OAuthClientProvider abstracts where credentials live (upstream
// OAuthClientProvider).
type OAuthClientProvider interface {
	RedirectURL() string
	ClientMetadata() OAuthClientMetadata
	ClientMetadataURL() *string
	State(ctx context.Context) (string, error)
	ClientInformation(ctx context.Context) (*OAuthClientInformation, error)
	SaveClientInformation(ctx context.Context, information *OAuthClientInformationFull) error
	Tokens(ctx context.Context) (*OAuthTokens, error)
	SaveTokens(ctx context.Context, tokens *OAuthTokens) error
	RedirectToAuthorization(ctx context.Context, url string) error
	SaveCodeVerifier(ctx context.Context, verifier string) error
	CodeVerifier(ctx context.Context) (string, error)
	AddClientAuthentication() AddClientAuthentication
	InvalidateCredentials(ctx context.Context, kind string) error
	SaveDiscoveryState(ctx context.Context, state *OAuthDiscoveryState) error
	DiscoveryState(ctx context.Context) (*OAuthDiscoveryState, error)
}

// FlowOptions configure the authorization flow (upstream OAuthFlowOptions).
type FlowOptions struct {
	ServerURL string
	// AuthorizationCode is the code from a browser redirect; when set the
	// flow exchanges it instead of starting an authorization.
	AuthorizationCode string
	// Iss is the `iss` parameter of the authorization response that
	// delivered AuthorizationCode (RFC 9207).
	Iss *string
	// Scope limits or extends the requested scope.
	Scope *string
	// ResourceMetadataURL points at a fixed RFC 9728 document.
	ResourceMetadataURL *string
	// AuthorizationServerMetadataURL is a metadata document used instead of
	// discovery; trusted as configured. Must use https, except on loopback.
	AuthorizationServerMetadataURL *string
	Fetch                          McpFetch
	SkipIssuerValidation           bool
	// SkipRefresh goes straight to the authorization redirect instead of
	// refreshing stored tokens, for example when the server asks for scopes
	// the current grant lacks (a refresh keeps the old scope).
	SkipRefresh bool
}

// FlowResult is the flow outcome (upstream OAuthFlowResult).
type FlowResult = string

// Flow results.
const (
	ResultAuthorized FlowResult = "AUTHORIZED"
	ResultRedirect   FlowResult = "REDIRECT"
)

// TokenRequestOptions carries the token-request inputs (upstream
// TokenRequestOptions).
type TokenRequestOptions struct {
	Metadata                *AuthorizationServerMetadata
	ClientInformation       *OAuthClientInformation
	Resource                *string
	AddClientAuthentication AddClientAuthentication
	Fetch                   McpFetch
}

func loopback(hostname string) bool {
	hostname = strings.Trim(hostname, "[]")
	return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

// secureEndpoint refuses to send credentials over plain HTTP (upstream
// secureEndpoint).
func secureEndpoint(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" && !loopback(parsed.Hostname()) {
		return nil, &OAuthInsecureEndpointError{Endpoint: parsed.String()}
	}
	return parsed, nil
}

// selectClientAuthMethod picks the token-endpoint auth method (upstream
// selectClientAuthMethod).
func selectClientAuthMethod(information *OAuthClientInformationFull, supported []string) string {
	if information.TokenEndpointAuthMethod != nil {
		hinted := *information.TokenEndpointAuthMethod
		known := hinted == ClientAuthSecretBasic || hinted == ClientAuthSecretPost || hinted == ClientAuthNone
		hasSecret := information.ClientSecret != nil
		if known && (len(supported) == 0 || contains(supported, hinted)) && (hinted != ClientAuthSecretBasic || hasSecret) {
			return hinted
		}
	}
	if len(supported) == 0 {
		if information.ClientSecret != nil {
			return ClientAuthSecretBasic
		}
		return ClientAuthNone
	}
	if information.ClientSecret != nil && contains(supported, ClientAuthSecretBasic) {
		return ClientAuthSecretBasic
	}
	if information.ClientSecret != nil && contains(supported, ClientAuthSecretPost) {
		return ClientAuthSecretPost
	}
	if contains(supported, ClientAuthNone) {
		return ClientAuthNone
	}
	if information.ClientSecret != nil {
		return ClientAuthSecretPost
	}
	return ClientAuthNone
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// applyClientAuthentication writes the auth into headers/params (upstream
// applyClientAuthentication).
func applyClientAuthentication(method string, information *OAuthClientInformation, headers map[string]string, params url.Values) error {
	if method == ClientAuthSecretBasic {
		if information.ClientSecret == nil {
			return fmt.Errorf("client_secret_basic requires a client secret")
		}
		auth := base64.StdEncoding.EncodeToString([]byte(information.ClientID + ":" + *information.ClientSecret))
		headers["Authorization"] = "Basic " + auth
		return nil
	}
	params.Set("client_id", information.ClientID)
	if method == ClientAuthSecretPost && information.ClientSecret != nil {
		params.Set("client_secret", *information.ClientSecret)
	}
	return nil
}

// PKCE generates the verifier/S256 pair (upstream pkce, WebCrypto).
func pkce() (verifier, challenge string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(bytes)
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// StartAuthorization builds the authorization URL (upstream
// startAuthorization).
func StartAuthorization(authorizationServerURL string, options StartAuthorizationOptions) (*AuthorizationDraft, error) {
	metadata := options.Metadata
	if metadata != nil && !contains(metadata.ResponseTypesSupported, "code") {
		return nil, fmt.Errorf("Authorization server does not support authorization codes")
	}
	if metadata != nil && metadata.CodeChallengeMethodsSupported != nil && !contains(metadata.CodeChallengeMethodsSupported, "S256") {
		return nil, fmt.Errorf("Authorization server does not support PKCE S256")
	}
	endpoint := joinPathForServer(authorizationServerURL, "/authorize")
	if metadata != nil {
		endpoint = metadata.AuthorizationEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	verifier, challenge, err := pkce()
	if err != nil {
		return nil, err
	}
	query.Set("response_type", "code")
	query.Set("client_id", options.ClientInformation.ClientID)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("redirect_uri", options.RedirectURL)
	if options.State != nil {
		query.Set("state", *options.State)
	}
	if options.Scope != nil {
		query.Set("scope", *options.Scope)
		if contains(strings.Fields(*options.Scope), "offline_access") {
			query.Set("prompt", "consent")
		}
	}
	if options.Resource != nil {
		query.Set("resource", *options.Resource)
	}
	parsed.RawQuery = query.Encode()
	return &AuthorizationDraft{AuthorizationURL: parsed.String(), CodeVerifier: verifier}, nil
}

// StartAuthorizationOptions are the startAuthorization inputs.
type StartAuthorizationOptions struct {
	Metadata          *AuthorizationServerMetadata
	ClientInformation *OAuthClientInformation
	RedirectURL       string
	Scope             *string
	State             *string
	Resource          *string
}

// AuthorizationDraft is the built authorization request.
type AuthorizationDraft struct {
	AuthorizationURL string
	CodeVerifier     string
}

func joinPathForServer(serverURL, path string) string {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return serverURL + path
	}
	clone := *parsed
	clone.Path = path
	return clone.String()
}

// tokenRequest posts the token form and parses the response (upstream
// tokenRequest). Servers may report OAuth errors with any status, so the
// body is checked before the status.
func tokenRequest(ctx context.Context, authorizationServerURL string, options TokenRequestOptions, params url.Values) (*OAuthTokens, error) {
	tokenEndpoint := joinPathForServer(authorizationServerURL, "/token")
	if options.Metadata != nil {
		tokenEndpoint = options.Metadata.TokenEndpoint
	}
	tokenURL, err := secureEndpoint(tokenEndpoint)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Accept":       "application/json",
		"content-type": "application/x-www-form-urlencoded",
	}
	if options.Resource != nil {
		params.Set("resource", *options.Resource)
	}
	if options.AddClientAuthentication != nil {
		if err := options.AddClientAuthentication(headers, params, tokenURL.String(), options.Metadata); err != nil {
			return nil, err
		}
	} else {
		supported := []string{}
		if options.Metadata != nil && options.Metadata.TokenEndpointAuthMethodsSupported != nil {
			supported = options.Metadata.TokenEndpointAuthMethodsSupported
		}
		hint := &OAuthClientInformationFull{OAuthClientInformation: *options.ClientInformation}
		if err := applyClientAuthentication(selectClientAuthMethod(hint, supported), options.ClientInformation, headers, params); err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL.String(), strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	fetch := options.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	response, err := fetch(ctx, request)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := unmarshalJSON(body, &decoded); err != nil {
		decoded = nil
	}
	if m, ok := decoded.(map[string]any); ok {
		if errorText, ok := m["error"].(string); ok && errorText != "" {
			description := errorText
			if text, ok := m["error_description"].(string); ok {
				description = text
			}
			var errorURI *string
			if uri, ok := m["error_uri"].(string); ok {
				errorURI = &uri
			}
			return nil, &OAuthError{Code: errorText, Msg: description, ErrorURI: errorURI}
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &OAuthError{Code: "server_error", Msg: fmt.Sprintf("HTTP %d: %s", response.StatusCode, string(body))}
	}
	return ParseOAuthTokens(decoded)
}

// RegisterClient performs dynamic client registration (upstream
// registerClient).
func RegisterClient(ctx context.Context, authorizationServerURL string, options RegisterClientOptions) (*OAuthClientInformationFull, error) {
	endpoint := joinPathForServer(authorizationServerURL, "/register")
	if options.Metadata != nil {
		if options.Metadata.RegistrationEndpoint == nil {
			return nil, fmt.Errorf("Authorization server does not support dynamic client registration")
		}
		endpoint = *options.Metadata.RegistrationEndpoint
	}
	body := options.ClientMetadata
	payload := map[string]any{}
	if enc, err := marshalJSON(body); err != nil {
		return nil, err
	} else if err := unmarshalJSON(enc, &payload); err != nil {
		return nil, err
	}
	if options.Scope != nil {
		payload["scope"] = *options.Scope
	}
	enc, err := marshalJSON(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(enc)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("content-type", "application/json")
	fetch := options.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	response, err := fetch(ctx, request)
	if err != nil {
		return nil, err
	}
	text, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &OAuthRegistrationError{Status: response.StatusCode, Body: string(text)}
	}
	var decoded any
	if err := unmarshalJSON(text, &decoded); err != nil {
		return nil, err
	}
	return ParseClientInformation(decoded)
}

// RegisterClientOptions are the registerClient inputs.
type RegisterClientOptions struct {
	Metadata       *AuthorizationServerMetadata
	ClientMetadata OAuthClientMetadata
	Scope          *string
	Fetch          McpFetch
}

// ExchangeAuthorizationCode swaps the browser-redirect code for tokens
// (upstream exchangeAuthorizationCode).
func ExchangeAuthorizationCode(ctx context.Context, authorizationServerURL string, options ExchangeOptions) (*OAuthTokens, error) {
	params := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {options.Code},
		"code_verifier": {options.CodeVerifier},
		"redirect_uri":  {options.RedirectURL},
	}
	return tokenRequest(ctx, authorizationServerURL, options.TokenRequestOptions, params)
}

// ExchangeOptions are the exchange inputs.
type ExchangeOptions struct {
	TokenRequestOptions
	Code         string
	CodeVerifier string
	RedirectURL  string
}

// RefreshAuthorization refreshes tokens (upstream refreshAuthorization); a
// response without `refresh_token` keeps the previous one (RFC 6749 §6).
func RefreshAuthorization(ctx context.Context, authorizationServerURL string, options RefreshOptions) (*OAuthTokens, error) {
	params := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {options.RefreshToken},
	}
	tokens, err := tokenRequest(ctx, authorizationServerURL, options.TokenRequestOptions, params)
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == nil {
		refresh := options.RefreshToken
		tokens.RefreshToken = &refresh
	}
	return tokens, nil
}

// RefreshOptions are the refresh inputs.
type RefreshOptions struct {
	TokenRequestOptions
	RefreshToken string
}

// withScope: a response without `scope` grants the requested scope (RFC
// 6749 §5.1).
func withScope(tokens *OAuthTokens, scope *string) *OAuthTokens {
	if tokens.Scope == nil && scope != nil {
		tokens.Scope = scope
	}
	return tokens
}

// StepUpScope computes step-up scopes: the challenged scopes plus the ones
// granted so far, since a challenge may list only the missing scopes and a
// token with just those would lose access the old one had (SEP-2350).
// Without challenged scopes, nil lets the flow pick its default.
func StepUpScope(granted, challenged *string) *string {
	if challenged == nil {
		return nil
	}
	seen := map[string]bool{}
	var scopes []string
	for _, scope := range []*string{granted, challenged} {
		if scope == nil {
			continue
		}
		for _, name := range strings.Fields(*scope) {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			scopes = append(scopes, name)
		}
	}
	joined := strings.Join(scopes, " ")
	return &joined
}

// runFlow is the authorization flow (upstream runFlow).
func runFlow(ctx context.Context, provider OAuthClientProvider, options FlowOptions) (FlowResult, error) {
	fetch := options.Fetch
	var metadataURL *string
	if options.AuthorizationServerMetadataURL != nil {
		secured, err := secureEndpoint(*options.AuthorizationServerMetadataURL)
		if err != nil {
			return "", err
		}
		text := secured.String()
		metadataURL = &text
	}
	// With a configured metadata URL, discovery is not cached, so changing
	// the URL applies at once.
	var discovered *OAuthServerInfo
	var cached *OAuthDiscoveryState
	if metadataURL == nil {
		cached, _ = provider.DiscoveryState(ctx)
	}
	if cached != nil && cached.AuthorizationServerURL != "" {
		discovered = &OAuthServerInfo{AuthorizationServerURL: cached.AuthorizationServerURL, ResourceMetadata: cached.ResourceMetadata}
		if cached.AuthorizationServerMetadata != nil {
			discovered.AuthorizationServerMetadata = cached.AuthorizationServerMetadata
		} else {
			metadata, err := DiscoverAuthorizationServerMetadata(ctx, cached.AuthorizationServerURL, DiscoveryOptions{
				Fetch:                fetch,
				SkipIssuerValidation: options.SkipIssuerValidation,
			})
			if err != nil {
				return "", err
			}
			discovered.AuthorizationServerMetadata = metadata
		}
	} else {
		info, err := DiscoverOAuthServerInfo(ctx, options.ServerURL, DiscoveryOptions{
			ResourceMetadataURL:            options.ResourceMetadataURL,
			AuthorizationServerMetadataURL: metadataURL,
			Fetch:                          fetch,
			SkipIssuerValidation:           options.SkipIssuerValidation,
		})
		if err != nil {
			return "", err
		}
		discovered = info
	}
	if metadataURL == nil {
		cached := &OAuthDiscoveryState{AuthorizationServerURL: discovered.AuthorizationServerURL, AuthorizationServerMetadata: discovered.AuthorizationServerMetadata, ResourceMetadata: discovered.ResourceMetadata}
		if options.ResourceMetadataURL != nil {
			text := *options.ResourceMetadataURL
			cached.ResourceMetadataURL = &text
		}
		if err := provider.SaveDiscoveryState(ctx, cached); err != nil {
			return "", err
		}
	}
	metadata := discovered.AuthorizationServerMetadata
	resource, err := SelectResource(options.ServerURL, discovered.ResourceMetadata)
	if err != nil {
		return "", err
	}
	// `||`, not `??`: an empty scope (for example from `scopes_supported:
	// []`) falls through to the next source.
	var scope *string
	if options.Scope != nil && *options.Scope != "" {
		scope = options.Scope
	} else if discovered.ResourceMetadata != nil && len(discovered.ResourceMetadata.ScopesSupported) > 0 {
		joined := strings.Join(discovered.ResourceMetadata.ScopesSupported, " ")
		scope = &joined
	} else if metadata := provider.ClientMetadata(); metadata.Scope != nil {
		scope = metadata.Scope
	}
	client, err := provider.ClientInformation(ctx)
	if err != nil {
		return "", err
	}
	if client == nil {
		if options.AuthorizationCode != "" {
			return "", fmt.Errorf("OAuth client information is missing during code exchange")
		}
		if metadata != nil && metadata.ClientIDMetadataDocumentSupported != nil && *metadata.ClientIDMetadataDocumentSupported && provider.ClientMetadataURL() != nil {
			documentURL, err := url.Parse(*provider.ClientMetadataURL())
			if err != nil || documentURL.Scheme != "https" || documentURL.Path == "/" {
				return "", fmt.Errorf("Invalid OAuth client metadata URL")
			}
			client = &OAuthClientInformation{ClientID: *provider.ClientMetadataURL()}
			if err := provider.SaveClientInformation(ctx, &OAuthClientInformationFull{OAuthClientInformation: *client}); err != nil {
				return "", err
			}
		} else {
			registered, err := RegisterClient(ctx, discovered.AuthorizationServerURL, RegisterClientOptions{
				Metadata: metadata, ClientMetadata: provider.ClientMetadata(), Scope: scope, Fetch: fetch,
			})
			if err != nil {
				return "", err
			}
			client = &registered.OAuthClientInformation
			if err := provider.SaveClientInformation(ctx, registered); err != nil {
				return "", err
			}
		}
	}
	tokenOptions := TokenRequestOptions{
		Metadata: metadata, ClientInformation: client, Resource: resource,
		AddClientAuthentication: provider.AddClientAuthentication(), Fetch: fetch,
	}
	if options.AuthorizationCode != "" {
		// RFC 9207: never send a code from another authorization server to
		// this one.
		// The metadata flag is a boolean upstream (truthiness), not
		// presence of the field.
		issPromised := metadata.AuthorizationResponseIssParameterSupported != nil && *metadata.AuthorizationResponseIssParameterSupported
		if metadata != nil && (options.Iss != nil || issPromised) {
			var received *string
			if options.Iss != nil && *options.Iss != "" {
				received = options.Iss
			}
			expected := metadata.Issuer
			if received == nil || *received != expected {
				return "", &OAuthIssuerMismatchError{Expected: expected, Received: received}
			}
		}
		verifier, err := provider.CodeVerifier(ctx)
		if err != nil {
			return "", err
		}
		tokens, err := ExchangeAuthorizationCode(ctx, discovered.AuthorizationServerURL, ExchangeOptions{
			TokenRequestOptions: tokenOptions,
			Code:                options.AuthorizationCode,
			CodeVerifier:        verifier,
			RedirectURL:         provider.RedirectURL(),
		})
		if err != nil {
			return "", err
		}
		if err := provider.SaveTokens(ctx, withScope(tokens, scope)); err != nil {
			return "", err
		}
		return ResultAuthorized, nil
	}
	var existing *OAuthTokens
	if !options.SkipRefresh {
		existing, err = provider.Tokens(ctx)
		if err != nil {
			return "", err
		}
	}
	if existing != nil && existing.RefreshToken != nil {
		tokens, refreshErr := RefreshAuthorization(ctx, discovered.AuthorizationServerURL, RefreshOptions{
			TokenRequestOptions: tokenOptions, RefreshToken: *existing.RefreshToken,
		})
		if refreshErr == nil {
			// A refresh without `scope` keeps the scope of the grant (RFC
			// 6749 §6).
			if err := provider.SaveTokens(ctx, withScope(tokens, existing.Scope)); err != nil {
				return "", err
			}
			return ResultAuthorized, nil
		}
		if insecure, ok := refreshErr.(*OAuthInsecureEndpointError); ok {
			_ = insecure
			return "", refreshErr
		}
		if oauthErr, ok := refreshErr.(*OAuthError); ok && oauthErr.Code != "server_error" {
			return "", refreshErr
		}
	}
	var state *string
	if value, err := provider.State(ctx); err == nil {
		state = &value
	} else {
		return "", err
	}
	authorization, err := StartAuthorization(discovered.AuthorizationServerURL, StartAuthorizationOptions{
		Metadata: metadata, ClientInformation: client, RedirectURL: provider.RedirectURL(),
		Scope: scope, State: state, Resource: resource,
	})
	if err != nil {
		return "", err
	}
	if err := provider.SaveCodeVerifier(ctx, authorization.CodeVerifier); err != nil {
		return "", err
	}
	if err := provider.RedirectToAuthorization(ctx, authorization.AuthorizationURL); err != nil {
		return "", err
	}
	return ResultRedirect, nil
}

// AuthorizeMcp runs the OAuth flow, retrying once after invalidating
// credentials on the recoverable OAuth errors (upstream authorizeMcp).
func AuthorizeMcp(ctx context.Context, provider OAuthClientProvider, options FlowOptions) (FlowResult, error) {
	result, err := runFlow(ctx, provider, options)
	if err == nil {
		return result, nil
	}
	if oauthErr, ok := err.(*OAuthError); ok {
		if oauthErr.Code == "invalid_client" || oauthErr.Code == "unauthorized_client" {
			if err := provider.InvalidateCredentials(ctx, "all"); err != nil {
				return "", err
			}
			return runFlow(ctx, provider, options)
		}
		if oauthErr.Code == "invalid_grant" {
			if err := provider.InvalidateCredentials(ctx, "tokens"); err != nil {
				return "", err
			}
			return runFlow(ctx, provider, options)
		}
	}
	return "", err
}
