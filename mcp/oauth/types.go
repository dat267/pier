// Package oauth is a Go port of @earendil-works/pi-mcp's oauth module
// (pi/packages/mcp/src/oauth), itself adapted from
// modelcontextprotocol/typescript-sdk v1.29.0 with dependency-free
// structural validation.
//
// Ground truth: pi/packages/mcp/src/oauth at the pinned upstream commit.
package oauth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// OAuthProtectedResourceMetadata is RFC 9728 (upstream
// OAuthProtectedResourceMetadata).
type OAuthProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`

	// Extra carries the unknown members of the document.
	Extra map[string]json.RawMessage `json:"-"`
}

// AuthorizationServerMetadata is RFC 8414/OpenID discovery (upstream
// AuthorizationServerMetadata).
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              *string  `json:"registration_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	ClientIDMetadataDocumentSupported *bool    `json:"client_id_metadata_document_supported,omitempty"`
	// AuthorizationResponseIssParameterSupported: authorization responses
	// carry an `iss` parameter (RFC 9207).
	AuthorizationResponseIssParameterSupported *bool `json:"authorization_response_iss_parameter_supported,omitempty"`

	Extra map[string]json.RawMessage `json:"-"`
}

// OAuthTokens is a token response (upstream OAuthTokens).
type OAuthTokens struct {
	AccessToken  string  `json:"access_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    *int64  `json:"expires_in,omitempty"`
	Scope        *string `json:"scope,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	IDToken      *string `json:"id_token,omitempty"`
}

// OAuthClientMetadata is the RFC 7591 client metadata (upstream
// OAuthClientMetadata).
type OAuthClientMetadata struct {
	RedirectURIs            []string        `json:"redirect_uris"`
	TokenEndpointAuthMethod *string         `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string        `json:"grant_types,omitempty"`
	ResponseTypes           []string        `json:"response_types,omitempty"`
	ClientName              *string         `json:"client_name,omitempty"`
	ClientURI               *string         `json:"client_uri,omitempty"`
	LogoURI                 *string         `json:"logo_uri,omitempty"`
	Scope                   *string         `json:"scope,omitempty"`
	Contacts                []string        `json:"contacts,omitempty"`
	TOSURI                  *string         `json:"tos_uri,omitempty"`
	PolicyURI               *string         `json:"policy_uri,omitempty"`
	JWKSURI                 *string         `json:"jwks_uri,omitempty"`
	JWKS                    json.RawMessage `json:"jwks,omitempty"`
	SoftwareID              *string         `json:"software_id,omitempty"`
	SoftwareVersion         *string         `json:"software_version,omitempty"`
	SoftwareStatement       *string         `json:"software_statement,omitempty"`
}

// OAuthClientInformation is the registered client identity (upstream
// OAuthClientInformation).
type OAuthClientInformation struct {
	ClientID              string  `json:"client_id"`
	ClientSecret          *string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      *int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt *int64  `json:"client_secret_expires_at,omitempty"`
}

// OAuthClientInformationFull is OAuthClientInformation + OAuthClientMetadata
// (upstream OAuthClientInformationFull).
type OAuthClientInformationFull struct {
	OAuthClientInformation
	OAuthClientMetadata
}

// OAuthDiscoveryState caches the discovered server documents (upstream
// OAuthDiscoveryState).
type OAuthDiscoveryState struct {
	AuthorizationServerURL      string                          `json:"authorizationServerUrl"`
	AuthorizationServerMetadata *AuthorizationServerMetadata    `json:"authorizationServerMetadata,omitempty"`
	ResourceMetadata            *OAuthProtectedResourceMetadata `json:"resourceMetadata,omitempty"`
	ResourceMetadataURL         *string                         `json:"resourceMetadataUrl,omitempty"`
}

// OAuthServerInfo is the discovery answer (upstream OAuthServerInfo).
type OAuthServerInfo struct {
	AuthorizationServerURL      string
	AuthorizationServerMetadata *AuthorizationServerMetadata
	ResourceMetadata            *OAuthProtectedResourceMetadata
}

// OAuthChallenge is the parsed www-authenticate challenge (upstream
// OAuthChallenge).
type OAuthChallenge struct {
	ResourceMetadataURL *string
	Scope               *string
	Error               *string
	ErrorDescription    *string
}

// --- structural validation -------------------------------------------------

func object(value any, name string) (map[string]any, error) {
	m, ok := value.(map[string]any)
	if !ok || m == nil {
		return nil, fmt.Errorf("Invalid %s", name)
	}
	return m, nil
}

// requiredString validates a non-empty string member.
func requiredString(input map[string]any, key, name string) (string, error) {
	value, _ := input[key].(string)
	if value == "" {
		return "", fmt.Errorf("Invalid %s", name)
	}
	return value, nil
}

// absent treats `null` and `""` as absent: servers send them for fields they
// have no value for, like `scope: ""`.
func absent(value any) bool {
	return value == nil || value == "" || value == (*string)(nil)
}

func optionalString(input map[string]any, key, name string) (*string, error) {
	if absent(input[key]) {
		return nil, nil
	}
	value, err := requiredString(input, key, name)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func optionalStrings(input map[string]any, key, name string) ([]string, error) {
	value, present := input[key]
	if !present || value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("Invalid %s", name)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("Invalid %s", name)
		}
		out = append(out, text)
	}
	return out, nil
}

// safeUrl validates a URL member: parseable and not a scripting scheme; URL
// parse failures are `TypeError` upstream, which discovery reserves for
// network failures.
func safeUrl(input map[string]any, key, name string) (string, error) {
	text, err := requiredString(input, key, name)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("Invalid %s", name)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "javascript:", "data:", "vbscript:":
		return "", fmt.Errorf("Invalid %s", name)
	}
	if strings.HasSuffix(strings.ToLower(parsed.Scheme), ":") {
		return "", fmt.Errorf("Invalid %s", name)
	}
	return text, nil
}

func optionalUrl(input map[string]any, key, name string) (*string, error) {
	if absent(input[key]) {
		return nil, nil
	}
	value, err := safeUrl(input, key, name)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// ParseProtectedResourceMetadata validates a resource-metadata document.
func ParseProtectedResourceMetadata(value any) (*OAuthProtectedResourceMetadata, error) {
	input, err := object(value, "OAuth protected resource metadata")
	if err != nil {
		return nil, err
	}
	resource, err := safeUrl(input, "resource", "OAuth protected resource metadata resource")
	if err != nil {
		return nil, err
	}
	result := &OAuthProtectedResourceMetadata{Resource: resource}
	if servers, err := optionalStrings(input, "authorization_servers", "authorization_servers"); err != nil {
		return nil, err
	} else if servers != nil {
		for _, server := range servers {
			safe, err := safeUrl(map[string]any{"url": server}, "url", "authorization server URL")
			if err != nil {
				return nil, err
			}
			result.AuthorizationServers = append(result.AuthorizationServers, safe)
		}
	}
	if scopes, err := optionalStrings(input, "scopes_supported", "scopes_supported"); err != nil {
		return nil, err
	} else {
		result.ScopesSupported = scopes
	}
	return result, nil
}

// ParseAuthorizationServerMetadata validates an authorization-server
// metadata document.
func ParseAuthorizationServerMetadata(value any) (*AuthorizationServerMetadata, error) {
	input, err := object(value, "authorization server metadata")
	if err != nil {
		return nil, err
	}
	responseTypes, err := optionalStrings(input, "response_types_supported", "response_types_supported")
	if err != nil {
		return nil, err
	}
	if responseTypes == nil {
		return nil, fmt.Errorf("Invalid response_types_supported")
	}
	issuer, err := safeUrl(input, "issuer", "authorization server issuer")
	if err != nil {
		return nil, err
	}
	authorizationEndpoint, err := safeUrl(input, "authorization_endpoint", "authorization endpoint")
	if err != nil {
		return nil, err
	}
	tokenEndpoint, err := safeUrl(input, "token_endpoint", "token endpoint")
	if err != nil {
		return nil, err
	}
	result := &AuthorizationServerMetadata{
		Issuer:                 issuer,
		AuthorizationEndpoint:  authorizationEndpoint,
		TokenEndpoint:          tokenEndpoint,
		ResponseTypesSupported: responseTypes,
	}
	if registration, err := optionalUrl(input, "registration_endpoint", "registration endpoint"); err != nil {
		return nil, err
	} else {
		result.RegistrationEndpoint = registration
	}
	if scopes, err := optionalStrings(input, "scopes_supported", "scopes_supported"); err != nil {
		return nil, err
	} else {
		result.ScopesSupported = scopes
	}
	if grants, err := optionalStrings(input, "grant_types_supported", "grant_types_supported"); err != nil {
		return nil, err
	} else {
		result.GrantTypesSupported = grants
	}
	if methods, err := optionalStrings(input, "token_endpoint_auth_methods_supported", "token_endpoint_auth_methods_supported"); err != nil {
		return nil, err
	} else {
		result.TokenEndpointAuthMethodsSupported = methods
	}
	if challenges, err := optionalStrings(input, "code_challenge_methods_supported", "code_challenge_methods_supported"); err != nil {
		return nil, err
	} else {
		result.CodeChallengeMethodsSupported = challenges
	}
	if flag, ok := input["client_id_metadata_document_supported"].(bool); ok {
		result.ClientIDMetadataDocumentSupported = &flag
	}
	if flag, ok := input["authorization_response_iss_parameter_supported"].(bool); ok {
		result.AuthorizationResponseIssParameterSupported = &flag
	}
	return result, nil
}

// ParseOAuthTokens validates a token response. `Number(null)` is 0 upstream,
// which would mark the token as expired at once; absent/null expires_in is
// treated as no expiry.
func ParseOAuthTokens(value any) (*OAuthTokens, error) {
	input, err := object(value, "OAuth token response")
	if err != nil {
		return nil, err
	}
	accessToken, err := requiredString(input, "access_token", "access_token")
	if err != nil {
		return nil, err
	}
	tokenType, err := requiredString(input, "token_type", "token_type")
	if err != nil {
		return nil, err
	}
	result := &OAuthTokens{AccessToken: accessToken, TokenType: tokenType}
	if !absent(input["expires_in"]) {
		number, ok := toFloat(input["expires_in"])
		if !ok {
			return nil, fmt.Errorf("Invalid expires_in")
		}
		result.ExpiresIn = &number
	}
	if scope, err := optionalString(input, "scope", "scope"); err != nil {
		return nil, err
	} else {
		result.Scope = scope
	}
	if refresh, err := optionalString(input, "refresh_token", "refresh_token"); err != nil {
		return nil, err
	} else {
		result.RefreshToken = refresh
	}
	if idToken, err := optionalString(input, "id_token", "id_token"); err != nil {
		return nil, err
	} else {
		result.IDToken = idToken
	}
	return result, nil
}

// ParseClientInformation validates a dynamic registration response.
func ParseClientInformation(value any) (*OAuthClientInformationFull, error) {
	input, err := object(value, "OAuth client registration response")
	if err != nil {
		return nil, err
	}
	clientID, err := requiredString(input, "client_id", "client_id")
	if err != nil {
		return nil, err
	}
	result := &OAuthClientInformationFull{}
	result.ClientID = clientID
	if secret, err := optionalString(input, "client_secret", "client_secret"); err != nil {
		return nil, err
	} else {
		result.ClientSecret = secret
	}
	if issued, ok := toFloat(input["client_id_issued_at"]); ok && !absent(input["client_id_issued_at"]) {
		result.ClientIDIssuedAt = &issued
	}
	if expires, ok := toFloat(input["client_secret_expires_at"]); ok && !absent(input["client_secret_expires_at"]) {
		result.ClientSecretExpiresAt = &expires
	}
	redirects, err := optionalStrings(input, "redirect_uris", "redirect_uris")
	if err != nil {
		return nil, err
	}
	if redirects == nil {
		redirects = []string{}
	}
	result.RedirectURIs = redirects
	// Remaining metadata members pass through (token_endpoint_auth_method,
	// grant_types, ...).
	if method, err := optionalString(input, "token_endpoint_auth_method", "token_endpoint_auth_method"); err == nil {
		result.TokenEndpointAuthMethod = method
	}
	if grants, err := optionalStrings(input, "grant_types", "grant_types"); err == nil {
		result.GrantTypes = grants
	}
	if types, err := optionalStrings(input, "response_types", "response_types"); err == nil {
		result.ResponseTypes = types
	}
	if name, err := optionalString(input, "client_name", "client_name"); err == nil {
		result.ClientName = name
	}
	if scope, err := optionalString(input, "scope", "scope"); err == nil {
		result.Scope = scope
	}
	return result, nil
}

func toFloat(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}
