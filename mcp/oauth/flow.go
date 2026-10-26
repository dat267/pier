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
	"slices"
	"strconv"
	"strings"
)

// Client auth methods (upstream ClientAuthMethod).
const (
	ClientAuthSecretBasic = "client_secret_basic"
	ClientAuthSecretPost  = "client_secret_post"
	ClientAuthNone        = "none"
)

// AddClientAuthentication customizes the token-request auth (upstream
// AddClientAuthentication). params is the ordered token form, so a custom
// callback sees the same insertion order the reference's URLSearchParams has.
type AddClientAuthentication func(headers map[string]string, params *OrderedForm, tokenURL string, metadata *AuthorizationServerMetadata) error

// OAuthClientMetadataDocument is a Client ID Metadata Document: an https URL
// used as `client_id`, and a redirect URI it lists (upstream
// OAuthClientMetadataDocument).
type OAuthClientMetadataDocument struct {
	URL         string
	RedirectURL string
}

// OAuthClientProvider abstracts where credentials live (upstream
// OAuthClientProvider).
type OAuthClientProvider interface {
	RedirectURL() string
	ClientMetadata() OAuthClientMetadata
	// ClientMetadataDocument chooses a Client ID Metadata Document to identify
	// as instead of registering dynamically, or nil to register. Called when no
	// client information is stored; the document is not stored. metadata is nil
	// when the authorization server has none.
	ClientMetadataDocument(metadata *AuthorizationServerMetadata) *OAuthClientMetadataDocument
	State(ctx context.Context) (string, error)
	ClientInformation(ctx context.Context) (*OAuthClientInformationFull, error)
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
	ClientInformation       *OAuthClientInformationFull
	Resource                *string
	AddClientAuthentication AddClientAuthentication
	Fetch                   McpFetch
}

func loopback(hostname string) bool {
	hostname = strings.ToLower(strings.Trim(hostname, "[]"))
	return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

// deriveApplicationType applies MCP SEP-837 to OAuth redirect URIs: custom
// schemes and loopback HTTP(S) URLs identify native clients; other URLs are web.
func parseHTTPURL(value string) (*url.URL, string, error) {
	value = stripWHATWGURLWhitespace(value)
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", fmt.Errorf("not an HTTP URL")
	}
	if parsed.Host == "" {
		colon := strings.IndexByte(value, ':')
		if colon < 0 {
			return nil, "", fmt.Errorf("not an absolute HTTP URL")
		}
		remainder := strings.ReplaceAll(value[colon+1:], string('\\'), "/")
		remainder = strings.TrimLeft(remainder, "/")
		if remainder == "" {
			return nil, "", fmt.Errorf("HTTP URL has no host")
		}
		parsed, err = url.Parse(parsed.Scheme + "://" + remainder)
		if err != nil || parsed.Host == "" {
			return nil, "", fmt.Errorf("HTTP URL has no host")
		}
	}
	if parsed.Hostname() == "" {
		return nil, "", fmt.Errorf("HTTP URL has no host")
	}
	if port := parsed.Port(); port != "" {
		number, portErr := strconv.Atoi(port)
		if portErr != nil || number < 0 || number > 65535 {
			return nil, "", fmt.Errorf("HTTP URL has an invalid port")
		}
	}
	return parsed, parsed.String(), nil
}

func deriveApplicationType(redirectURIs []string) string {
	for _, redirectURI := range redirectURIs {
		parsed, err := parseWHATWGURL(redirectURI)
		if err != nil || parsed.Scheme == "" {
			continue
		}
		scheme := strings.ToLower(parsed.Scheme)
		if (scheme == "http" || scheme == "https") && parsed.Host == "" {
			parsed, _, err = parseHTTPURL(redirectURI)
			if err != nil {
				continue
			}
		}
		if (scheme != "http" && scheme != "https") || loopback(strings.ToLower(parsed.Hostname())) {
			return "native"
		}
	}
	return "web"
}

// secureEndpoint refuses to send credentials over plain HTTP (upstream
// secureEndpoint).
func secureEndpoint(value string) (*url.URL, error) {
	parsed, _, err := parseHTTPURL(value)
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
		if known && (len(supported) == 0 || contains(supported, hinted)) {
			return hinted
		}
	}
	hasSecret := information.ClientSecret != nil && *information.ClientSecret != ""
	if len(supported) == 0 {
		if hasSecret {
			return ClientAuthSecretBasic
		}
		return ClientAuthNone
	}
	if hasSecret && contains(supported, ClientAuthSecretBasic) {
		return ClientAuthSecretBasic
	}
	if hasSecret && contains(supported, ClientAuthSecretPost) {
		return ClientAuthSecretPost
	}
	if contains(supported, ClientAuthNone) {
		return ClientAuthNone
	}
	if hasSecret {
		return ClientAuthSecretPost
	}
	return ClientAuthNone
}

func contains(list []string, value string) bool { return slices.Contains(list, value) }

// applyClientAuthentication writes the auth into headers/params (upstream
// applyClientAuthentication).
func applyClientAuthentication(method string, information *OAuthClientInformation, headers map[string]string, params *OrderedForm) error {
	if method == ClientAuthSecretBasic {
		if information.ClientSecret == nil || *information.ClientSecret == "" {
			return fmt.Errorf("client_secret_basic requires a client secret")
		}
		auth := base64.StdEncoding.EncodeToString([]byte(information.ClientID + ":" + *information.ClientSecret))
		headers["Authorization"] = "Basic " + auth
		return nil
	}
	params.Set("client_id", information.ClientID)
	if method == ClientAuthSecretPost && information.ClientSecret != nil && *information.ClientSecret != "" {
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
	parsed, err := parseWHATWGURL(endpoint)
	if err != nil {
		return nil, err
	}
	// URLSearchParams keeps the endpoint's query entries in place and appends
	// new names in assignment order; url.Values would sort and escape
	// differently.
	query := NewOrderedForm(parsed.RawQuery)
	verifier, challenge, err := pkce()
	if err != nil {
		return nil, err
	}
	query.Set("response_type", "code")
	query.Set("client_id", options.ClientInformation.ClientID)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("redirect_uri", options.RedirectURL)
	if options.State != nil && *options.State != "" {
		query.Set("state", *options.State)
	}
	if options.Scope != nil && *options.Scope != "" {
		query.Set("scope", *options.Scope)
		if contains(splitJSScope(*options.Scope), "offline_access") {
			query.Set("prompt", "consent")
		}
	}
	if options.Resource != nil && *options.Resource != "" {
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
	parsed, err := parseWHATWGURL(serverURL)
	if err != nil {
		return serverURL + path
	}
	clone := *parsed
	clone.Path = path
	clone.RawPath = ""
	clone.RawQuery = ""
	clone.ForceQuery = false
	clone.Fragment = ""
	clone.RawFragment = ""
	return clone.String()
}

// tokenRequest posts the token form and parses the response (upstream
// tokenRequest). Servers may report OAuth errors with any status, so the
// body is checked before the status.
func tokenRequest(ctx context.Context, authorizationServerURL string, options TokenRequestOptions, params *OrderedForm) (*OAuthTokens, error) {
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
	if options.Resource != nil && *options.Resource != "" {
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
		if err := applyClientAuthentication(selectClientAuthMethod(options.ClientInformation, supported), &options.ClientInformation.OAuthClientInformation, headers, params); err != nil {
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
		if errorText, ok := m["error"].(string); ok {
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
	// Upstream spreads the client metadata, inserts `application_type` in place
	// (or appends it) and appends a truthy `scope`; a Go struct keeps the same
	// members but not their order, so they are assembled explicitly.
	members := make([]jsonMember, 0, 18)
	add := func(name string, value any) { members = append(members, jsonMember{name: name, value: value}) }
	addString := func(name string, value *string) {
		if value != nil {
			add(name, *value)
		}
	}
	addStrings := func(name string, values []string) {
		if len(values) > 0 {
			add(name, values)
		}
	}
	// The provider fills client_name first, then redirect_uris, grant_types,
	// response_types and token_endpoint_auth_method (NewMcpOAuthProvider).
	addString("client_name", body.ClientName)
	add("redirect_uris", body.RedirectURIs)
	addStrings("grant_types", body.GrantTypes)
	addStrings("response_types", body.ResponseTypes)
	addString("token_endpoint_auth_method", body.TokenEndpointAuthMethod)
	addString("client_uri", body.ClientURI)
	addString("logo_uri", body.LogoURI)
	scopeIndex := -1
	if body.Scope != nil {
		add("scope", *body.Scope)
		scopeIndex = len(members) - 1
	}
	addStrings("contacts", body.Contacts)
	addString("tos_uri", body.TOSURI)
	addString("policy_uri", body.PolicyURI)
	addString("jwks_uri", body.JWKSURI)
	if len(body.JWKS) > 0 {
		add("jwks", body.JWKS)
	}
	addString("software_id", body.SoftwareID)
	addString("software_version", body.SoftwareVersion)
	addString("software_statement", body.SoftwareStatement)
	applicationType := deriveApplicationType(body.RedirectURIs)
	if body.ApplicationType != nil {
		applicationType = *body.ApplicationType
	}
	add("application_type", applicationType)
	// options.Scope overrides the metadata's scope in place, or appends it.
	if options.Scope != nil && *options.Scope != "" {
		if scopeIndex >= 0 {
			members[scopeIndex] = jsonMember{name: "scope", value: *options.Scope}
		} else {
			add("scope", *options.Scope)
		}
	}
	enc, err := marshalOrderedJSONObject(members)
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
	params := NewOrderedForm("")
	params.Set("grant_type", "authorization_code")
	params.Set("code", options.Code)
	params.Set("code_verifier", options.CodeVerifier)
	params.Set("redirect_uri", options.RedirectURL)
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
	params := NewOrderedForm("")
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", options.RefreshToken)
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
	if tokens.Scope == nil && scope != nil && *scope != "" {
		tokens.Scope = scope
	}
	return tokens
}

// StepUpScope computes step-up scopes: the challenged scopes plus the ones
// granted so far, since a challenge may list only the missing scopes and a
// token with just those would lose access the old one had (SEP-2350).
// Without challenged scopes, nil lets the flow pick its default.
func StepUpScope(granted, challenged *string) *string {
	// Upstream's falsy check treats a present-but-empty scope as absent.
	if challenged == nil || *challenged == "" {
		return nil
	}
	seen := map[string]bool{}
	var scopes []string
	for _, scope := range []*string{granted, challenged} {
		if scope == nil {
			continue
		}
		for _, name := range splitJSScope(*scope) {
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
	if options.AuthorizationServerMetadataURL != nil && *options.AuthorizationServerMetadataURL != "" {
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
		if joined != "" {
			scope = &joined
		}
	}
	if scope == nil {
		if metadata := provider.ClientMetadata(); metadata.Scope != nil {
			scope = metadata.Scope
		}
	}
	client, err := provider.ClientInformation(ctx)
	if err != nil {
		return "", err
	}
	var clientDocument *OAuthClientMetadataDocument
	if client == nil {
		clientDocument = provider.ClientMetadataDocument(metadata)
	}
	if clientDocument != nil {
		documentURL, _, err := parseHTTPURL(clientDocument.URL)
		if err != nil || documentURL.Scheme != "https" || documentURL.Path == "" || documentURL.Path == "/" {
			return "", fmt.Errorf("Invalid OAuth client metadata URL")
		}
	}
	if client == nil && clientDocument != nil {
		client = &OAuthClientInformationFull{OAuthClientInformation: OAuthClientInformation{ClientID: clientDocument.URL}}
	}
	if client == nil {
		if options.AuthorizationCode != "" {
			return "", fmt.Errorf("OAuth client information is missing during code exchange")
		}
		registered, err := RegisterClient(ctx, discovered.AuthorizationServerURL, RegisterClientOptions{
			Metadata: metadata, ClientMetadata: provider.ClientMetadata(), Scope: scope, Fetch: fetch,
		})
		if err != nil {
			return "", err
		}
		client = registered
		if err := provider.SaveClientInformation(ctx, registered); err != nil {
			return "", err
		}
	}
	// The document's redirect URI may differ from the provider's, for example
	// by a server-specific path.
	redirectURL := provider.RedirectURL()
	if clientDocument != nil {
		redirectURL = clientDocument.RedirectURL
	}
	tokenOptions := TokenRequestOptions{
		Metadata: metadata, ClientInformation: client, Resource: resource,
		AddClientAuthentication: provider.AddClientAuthentication(), Fetch: fetch,
	}
	if options.AuthorizationCode != "" {
		// RFC 9207: never send a code from another authorization server to
		// this one. Without metadata there is no issuer to compare with, so any
		// `iss` is rejected. The metadata flag is a boolean upstream
		// (truthiness), not presence of the field.
		issPromised := metadata != nil && metadata.AuthorizationResponseIssParameterSupported != nil && *metadata.AuthorizationResponseIssParameterSupported
		if options.Iss != nil || issPromised {
			if metadata == nil {
				return "", &OAuthIssuerMismatchError{Received: options.Iss}
			}
			received := ""
			if options.Iss != nil {
				received = *options.Iss
			}
			if options.Iss == nil || received != metadata.Issuer {
				return "", &OAuthIssuerMismatchError{Expected: metadata.Issuer, Received: options.Iss}
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
			RedirectURL:         redirectURL,
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
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if _, ok := refreshErr.(*OAuthInsecureEndpointError); ok {
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
		Metadata: metadata, ClientInformation: &client.OAuthClientInformation, RedirectURL: redirectURL,
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
