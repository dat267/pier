package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Port of the Anthropic SDK's OIDC workload identity federation token
// exchange, which upstream pi delegates to the SDK for the
// ANTHROPIC_FEDERATION_* environment (commit a9424cd43). The port has no
// Anthropic SDK, so the documented RFC 7523 jwt-bearer exchange is
// implemented here.

// Anthropic workload identity federation environment variables.
const (
	AnthropicFederationRuleIDEnv  = "ANTHROPIC_FEDERATION_RULE_ID"
	AnthropicOrganizationIDEnv    = "ANTHROPIC_ORGANIZATION_ID"
	AnthropicServiceAccountIDEnv  = "ANTHROPIC_SERVICE_ACCOUNT_ID"
	AnthropicIdentityTokenFileEnv = "ANTHROPIC_IDENTITY_TOKEN_FILE"
	AnthropicWorkspaceIDEnv       = "ANTHROPIC_WORKSPACE_ID"
)

// Federation protocol constants.
const (
	FederationTokenEndpoint = "/v1/oauth/token"
	GrantTypeJWTBearer      = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	OAuthAPIBetaHeader      = "oauth-2025-04-20"
	FederationBetaHeader    = "oidc-federation-2026-04-01"
	// MaxIdentityTokenBytes is the assertion limit the token endpoint enforces.
	MaxIdentityTokenBytes = 16 * 1024
	// MaxTokenResponseBytes bounds the token endpoint response body.
	MaxTokenResponseBytes = 1 << 20
	maxErrorBodyChars     = 2000
)

// WorkloadIdentityError is a federation failure.
type WorkloadIdentityError struct {
	Message   string
	Status    int
	Body      string
	RequestID string
}

func (e *WorkloadIdentityError) Error() string { return e.Message }

// FederationConfig is one OIDC federation token exchange.
type FederationConfig struct {
	IdentityTokenFile string
	FederationRuleID  string
	OrganizationID    string
	ServiceAccountID  string
	WorkspaceID       string
	BaseURL           string
	UserAgent         string
	HTTPClient        *http.Client
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

// FederationToken is one exchanged access token.
type FederationToken struct {
	Token string
	// ExpiresAtUnix is the token expiry as Unix seconds.
	ExpiresAtUnix int64
}

// ResolveFederationConfig reads the federation configuration from the
// ANTHROPIC_* provider environment; ok is false unless the three required
// variables are present.
func ResolveFederationConfig(env ProviderEnv) (*FederationConfig, bool) {
	ruleID, hasRule := GetProviderEnvValue(AnthropicFederationRuleIDEnv, env)
	organization, hasOrganization := GetProviderEnvValue(AnthropicOrganizationIDEnv, env)
	tokenFile, hasFile := GetProviderEnvValue(AnthropicIdentityTokenFileEnv, env)
	if !hasRule || !hasOrganization || !hasFile {
		return nil, false
	}
	config := &FederationConfig{
		IdentityTokenFile: tokenFile, FederationRuleID: ruleID, OrganizationID: organization,
	}
	if value, present := GetProviderEnvValue(AnthropicServiceAccountIDEnv, env); present {
		config.ServiceAccountID = value
	}
	if value, present := GetProviderEnvValue(AnthropicWorkspaceIDEnv, env); present {
		config.WorkspaceID = value
	}
	return config, true
}

// RequireSecureTokenEndpoint rejects a base URL that would send the assertion
// over cleartext HTTP; loopback hosts are allowed for local development.
func RequireSecureTokenEndpoint(baseURL string) error {
	if baseURL == "" {
		return nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return &WorkloadIdentityError{Message: fmt.Sprintf("Invalid token endpoint base URL %q: %v", baseURL, err)}
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return nil
	}
	return &WorkloadIdentityError{Message: fmt.Sprintf("Refusing to send credential over non-https token endpoint %q", baseURL)}
}

// RedactSensitive returns a redacted copy of a token-endpoint response body
// for safe inclusion in an error: strings are truncated and objects keep only
// the RFC 6749 §5.2 error fields.
func RedactSensitive(body string) string {
	if body == "" {
		return ""
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		if len(body) <= maxErrorBodyChars {
			return body
		}
		return body[:maxErrorBodyChars] + fmt.Sprintf("... <%d more chars>", len(body)-maxErrorBodyChars)
	}
	redacted := redactValue(parsed)
	encoded, err := MarshalJSON(redacted)
	if err != nil {
		return body
	}
	return string(encoded)
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for _, key := range []string{"error", "error_description", "error_uri"} {
			if entry, present := typed[key]; present {
				out[key] = entry
			}
		}
		return out
	case []any:
		return nil
	default:
		return typed
	}
}

// ExchangeFederationToken exchanges the identity token for a short-lived
// Anthropic access token (RFC 7523 jwt-bearer grant).
func ExchangeFederationToken(ctx context.Context, config FederationConfig) (FederationToken, error) {
	if err := RequireSecureTokenEndpoint(config.BaseURL); err != nil {
		return FederationToken{}, err
	}
	assertion, err := readIdentityToken(config.IdentityTokenFile)
	if err != nil {
		return FederationToken{}, err
	}
	if len(assertion) > MaxIdentityTokenBytes {
		return FederationToken{}, &WorkloadIdentityError{Message: fmt.Sprintf(
			"Identity token is %d KiB, exceeds the 16 KiB assertion limit", (len(assertion)+1023)/1024)}
	}
	body := map[string]string{
		"grant_type": GrantTypeJWTBearer, "assertion": assertion,
		"federation_rule_id": config.FederationRuleID, "organization_id": config.OrganizationID,
	}
	if config.ServiceAccountID != "" {
		body["service_account_id"] = config.ServiceAccountID
	}
	if config.WorkspaceID != "" {
		body["workspace_id"] = config.WorkspaceID
	}
	encoded, err := MarshalJSON(body)
	if err != nil {
		return FederationToken{}, err
	}
	endpoint := config.BaseURL + FederationTokenEndpoint
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return FederationToken{}, &WorkloadIdentityError{Message: fmt.Sprintf("Failed to reach token endpoint %s: %v", endpoint, err)}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-beta", OAuthAPIBetaHeader+","+FederationBetaHeader)
	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = "pi go oidcFederationProvider"
	}
	request.Header.Set("User-Agent", userAgent)
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return FederationToken{}, &WorkloadIdentityError{Message: fmt.Sprintf("Failed to reach token endpoint %s: %v", endpoint, err)}
	}
	defer response.Body.Close()
	text, err := readLimitedText(response.Body, MaxTokenResponseBytes)
	if err != nil {
		return FederationToken{}, err
	}
	requestID := response.Header.Get("Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		redacted := RedactSensitive(text)
		hint := ""
		if response.StatusCode == 401 {
			middle := ""
			if config.WorkspaceID == "" {
				middle = "If your federation rule is scoped to multiple workspaces, set the ANTHROPIC_WORKSPACE_ID environment variable or the workspace_id option. "
			}
			hint = " Ensure your federation rule matches your identity token. " + middle +
				"View your authentication events in the Workload identity page of Claude Console for more details."
		}
		suffix := ""
		if requestID != "" {
			suffix = fmt.Sprintf(" (request-id %s)", requestID)
		}
		return FederationToken{}, &WorkloadIdentityError{
			Message:   fmt.Sprintf("Token exchange failed with status %d%s: %s%s", response.StatusCode, suffix, redacted, hint),
			Status:    response.StatusCode,
			Body:      redacted,
			RequestID: requestID,
		}
	}
	var decoded struct {
		AccessToken string          `json:"access_token"`
		TokenType   string          `json:"token_type"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return FederationToken{}, &WorkloadIdentityError{
			Message:   fmt.Sprintf("Token endpoint returned non-JSON response (status %d)", response.StatusCode),
			Status:    response.StatusCode,
			Body:      RedactSensitive(text),
			RequestID: requestID,
		}
	}
	if decoded.AccessToken == "" {
		return FederationToken{}, &WorkloadIdentityError{
			Message:   fmt.Sprintf("Token endpoint response missing access_token: %s", RedactSensitive(text)),
			Status:    response.StatusCode,
			Body:      RedactSensitive(text),
			RequestID: requestID,
		}
	}
	if decoded.TokenType != "" && strings.ToLower(decoded.TokenType) != "bearer" {
		return FederationToken{}, &WorkloadIdentityError{
			Message:   fmt.Sprintf("Token endpoint response: unsupported token_type %q (want Bearer)", decoded.TokenType),
			Status:    response.StatusCode,
			Body:      RedactSensitive(text),
			RequestID: requestID,
		}
	}
	expiresIn, err := strconv.ParseFloat(strings.TrimSpace(string(decoded.ExpiresIn)), 64)
	if err != nil || decoded.ExpiresIn == nil {
		return FederationToken{}, &WorkloadIdentityError{
			Message:   fmt.Sprintf("Token endpoint response missing required fields: %s", RedactSensitive(text)),
			Status:    response.StatusCode,
			Body:      RedactSensitive(text),
			RequestID: requestID,
		}
	}
	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	return FederationToken{Token: decoded.AccessToken, ExpiresAtUnix: now().Unix() + int64(expiresIn)}, nil
}

func readIdentityToken(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", &WorkloadIdentityError{Message: fmt.Sprintf("Failed to read identity token file %s: %v", path, err)}
	}
	return strings.TrimSpace(string(content)), nil
}

func readLimitedText(reader io.Reader, limit int64) (string, error) {
	content, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// FederationTokenCache reuses an exchanged token across requests, refreshing
// it before expiry (the SDK reuses one client so its token cache avoids an
// exchange per request).
type FederationTokenCache struct {
	mu     sync.Mutex
	config FederationConfig
	token  *FederationToken
	// RefreshBackoff is the advisory window before expiry, in seconds.
	RefreshBackoff int64
}

// NewFederationTokenCache builds a cache over one federation config.
func NewFederationTokenCache(config FederationConfig) *FederationTokenCache {
	return &FederationTokenCache{config: config, RefreshBackoff: 5}
}

// Token returns a cached or freshly exchanged access token.
func (c *FederationTokenCache) Token(ctx context.Context) (string, error) {
	now := time.Now
	if c.config.Now != nil {
		now = c.config.Now
	}
	c.mu.Lock()
	if c.token != nil && now().Unix()+c.RefreshBackoff < c.token.ExpiresAtUnix {
		token := c.token.Token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()
	exchanged, err := ExchangeFederationToken(ctx, c.config)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token = &exchanged
	c.mu.Unlock()
	return exchanged.Token, nil
}
