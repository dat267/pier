package oauth

import (
	"cmp"
	"fmt"

	"github.com/dat267/pier/ai"
)

// OAuthError is a protocol-level OAuth failure with a code (upstream
// OAuthError).
type OAuthError struct {
	Code     string
	ErrorURI *string
	Msg      string
}

func (e *OAuthError) Error() string {
	return cmp.Or(e.Msg, e.Code)
}

// OAuthIssuerMismatchError reports an RFC 9207 issuer mismatch (upstream
// OAuthIssuerMismatchError). Received is nil when an authorization response
// lacked the `iss` parameter its server promised.
type OAuthIssuerMismatchError struct {
	Expected string
	Received *string
	Msg      string
}

func (e *OAuthIssuerMismatchError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	// The issuers are quoted like the reference's JSON.stringify (errors.ts:20),
	// so a quote, backslash or control character from the server cannot break up
	// the message.
	received := "none"
	if e.Received != nil {
		received = ai.QuoteJSON(*e.Received)
	}
	return fmt.Sprintf("OAuth issuer mismatch: expected %s, received %s", ai.QuoteJSON(e.Expected), received)
}

// OAuthInsecureEndpointError refuses to send credentials over plain HTTP
// (upstream OAuthInsecureEndpointError).
type OAuthInsecureEndpointError struct {
	Endpoint string
}

func (e *OAuthInsecureEndpointError) Error() string {
	return fmt.Sprintf("Refusing to send OAuth credentials to non-HTTPS endpoint %s", e.Endpoint)
}

// OAuthRegistrationError reports a failed dynamic client registration
// (upstream OAuthRegistrationError).
type OAuthRegistrationError struct {
	Status int
	Body   string
}

func (e *OAuthRegistrationError) Error() string {
	return fmt.Sprintf("OAuth dynamic client registration failed with status %d: %s", e.Status, e.Body)
}

// McpOAuthAuthorizationRequiredError: user interaction is needed (upstream
// McpOAuthAuthorizationRequiredError).
type McpOAuthAuthorizationRequiredError struct{}

func (e *McpOAuthAuthorizationRequiredError) Error() string {
	return "MCP OAuth authorization requires user interaction"
}
