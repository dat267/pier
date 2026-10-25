package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dat267/pier/mcp/protocol"
)

// McpFetch performs HTTP requests (upstream's fetch injection).
type McpFetch func(ctx context.Context, request *http.Request) (*http.Response, error)

// defaultRequestTimeout bounds one discovery fetch that does not carry its own
// deadline, matching the 30s MCP request timeout (D255).
var defaultRequestTimeout = 30 * time.Second

// cancelOnCloseBody releases a request context when the caller is done with the
// response.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// defaultFetch is the platform client, called without a receiver (#10188). Its
// response is bounded by defaultRequestTimeout or the caller's earlier
// deadline, and closing the body releases the timer.
func defaultFetch(ctx context.Context, request *http.Request) (*http.Response, error) {
	requestCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	response, err := http.DefaultClient.Do(request.WithContext(requestCtx))
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

// LatestProtocolVersion re-exports the MCP version for metadata requests.
const LatestProtocolVersion = protocol.LatestProtocolVersion

func isJSWhitespace(r rune) bool {
	return r == '\uFEFF' || (r != '\u0085' && unicode.IsSpace(r))
}

func splitJSScope(value string) []string {
	return strings.FieldsFunc(value, isJSWhitespace)
}

func discard(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
}

// isDiscoveryMiss: 4xx and 502 mean "not here", so discovery tries the next
// candidate URL.
func isDiscoveryMiss(status int) bool {
	return status >= 400 && status < 500 || status == 502
}

// pathSuffix is the path part of a `/.well-known/<kind><path>` URL; empty
// for the root path.
func pathSuffix(pathname string) string {
	if strings.HasSuffix(pathname, "/") {
		return strings.TrimSuffix(pathname, "/")
	}
	return pathname
}

const jsWhitespaceClass = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// field pulls one auth-param out of a challenge header; an empty value
// (`scope=""`) carries no information and counts as absent.
func field(header, name string) *string {
	// Parameter names are ASCII literals. Go's (?i) would also match Unicode
	// folds such as long s, unlike JavaScript's /i without /u.
	var pattern strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' {
			pattern.WriteByte('[')
			pattern.WriteRune(char)
			pattern.WriteRune(char - ('a' - 'A'))
			pattern.WriteByte(']')
		} else {
			pattern.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	re, err := regexp.Compile(`(?:^|[,` + jsWhitespaceClass + `])` + pattern.String() + `=(?:"([^"]*)"|([^` + jsWhitespaceClass + `,]+))`)
	if err != nil {
		return nil
	}
	match := re.FindStringSubmatch(header)
	if match == nil {
		return nil
	}
	value := match[1]
	if value == "" {
		value = match[2]
	}
	if value == "" {
		return nil
	}
	return &value
}

// ParseWwwAuthenticate parses a bearer (or dpop) challenge (upstream
// parseWwwAuthenticate).
func ParseWwwAuthenticate(header string) OAuthChallenge {
	if header == "" {
		return OAuthChallenge{}
	}
	scheme := strings.TrimLeftFunc(header, isJSWhitespace)
	if end := strings.IndexFunc(scheme, isJSWhitespace); end >= 0 {
		scheme = scheme[:end]
	}
	if scheme == "" {
		return OAuthChallenge{}
	}
	scheme = strings.ToLower(scheme)
	if scheme != "bearer" && scheme != "dpop" {
		return OAuthChallenge{}
	}
	challenge := OAuthChallenge{
		Scope:            field(header, "scope"),
		Error:            field(header, "error"),
		ErrorDescription: field(header, "error_description"),
	}
	if resourceMetadata := field(header, "resource_metadata"); resourceMetadata != nil {
		if parsed, err := url.Parse(*resourceMetadata); err == nil && parsed.Scheme != "" {
			challenge.ResourceMetadataURL = resourceMetadata
		}
	}
	return challenge
}

func fetchMetadata(ctx context.Context, fetch McpFetch, rawURL *url.URL, protocolVersion string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("MCP-Protocol-Version", protocolVersion)
	return fetch(ctx, request)
}

// DiscoverProtectedResourceMetadata loads the RFC 9728 document (upstream
// discoverProtectedResourceMetadata).
func DiscoverProtectedResourceMetadata(ctx context.Context, serverURL string, options DiscoveryOptions) (*OAuthProtectedResourceMetadata, error) {
	fetch := options.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	version := options.ProtocolVersion
	if version == "" {
		version = LatestProtocolVersion
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	target := url.URL{Scheme: server.Scheme, Host: server.Host}
	serverPath := server.EscapedPath()
	if options.ResourceMetadataURL != nil {
		configured, err := url.Parse(*options.ResourceMetadataURL)
		if err != nil {
			return nil, err
		}
		target = *configured
	} else if suffix := pathSuffix(serverPath); suffix != "" {
		// Upstream joins the well-known path onto the origin, replacing the
		// server path (and dropping userinfo, query and fragment).
		setEscapedPath(&target, "/.well-known/oauth-protected-resource"+suffix)
	} else {
		target.Path = "/.well-known/oauth-protected-resource"
	}
	response, err := fetchMetadata(ctx, fetch, &target, version)
	if err != nil {
		return nil, err
	}
	if options.ResourceMetadataURL == nil && serverPath != "" && serverPath != "/" && isDiscoveryMiss(response.StatusCode) {
		discard(response)
		fallback := url.URL{Scheme: server.Scheme, Host: server.Host, Path: "/.well-known/oauth-protected-resource"}
		response, err = fetchMetadata(ctx, fetch, &fallback, version)
		if err != nil {
			return nil, err
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := response.StatusCode
		discard(response)
		return nil, &statusError{message: fmt.Sprintf("HTTP %d loading OAuth protected resource metadata", status)}
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := unmarshalJSON(body, &decoded); err != nil {
		return nil, err
	}
	return ParseProtectedResourceMetadata(decoded)
}

// DiscoveryOptions carries the shared discovery parameters (upstream's
// per-function options objects).
type DiscoveryOptions struct {
	ResourceMetadataURL            *string
	AuthorizationServerMetadataURL *string
	Fetch                          McpFetch
	ProtocolVersion                string
	SkipIssuerValidation           bool
}

// BuildAuthorizationServerDiscoveryUrls lists the well-known candidates for
// an issuer (upstream buildAuthorizationServerDiscoveryUrls).
func BuildAuthorizationServerDiscoveryUrls(authorizationServerURL string) []struct {
	URL  string
	Type string // "oauth" | "oidc"
} {
	issuer, err := url.Parse(authorizationServerURL)
	if err != nil {
		return nil
	}
	path := pathSuffix(issuer.EscapedPath())
	urls := []struct {
		URL  string
		Type string
	}{
		{joinPath(*issuer, "/.well-known/oauth-authorization-server"+path), "oauth"},
		{joinPath(*issuer, "/.well-known/openid-configuration"+path), "oidc"},
	}
	if path != "" {
		urls = append(urls, struct {
			URL  string
			Type string
		}{joinPath(*issuer, path+"/.well-known/openid-configuration"), "oidc"})
	}
	return urls
}

func joinPath(base url.URL, suffix string) string {
	// Upstream resolves against the issuer's origin, which drops userinfo,
	// query and fragment while retaining the URL.pathname's escaped form.
	origin := url.URL{Scheme: base.Scheme, Host: base.Host}
	setEscapedPath(&origin, suffix)
	return origin.String()
}

func setEscapedPath(target *url.URL, escapedPath string) {
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil {
		target.Path = escapedPath
		target.RawPath = ""
		return
	}
	target.Path = decoded
	target.RawPath = escapedPath
}

// DiscoverAuthorizationServerMetadata walks the well-known candidates
// (upstream discoverAuthorizationServerMetadata).
func DiscoverAuthorizationServerMetadata(ctx context.Context, authorizationServerURL string, options DiscoveryOptions) (*AuthorizationServerMetadata, error) {
	fetch := options.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	for _, candidate := range BuildAuthorizationServerDiscoveryUrls(authorizationServerURL) {
		parsed, err := url.Parse(candidate.URL)
		if err != nil {
			continue
		}
		response, err := fetchMetadata(ctx, fetch, parsed, options.ProtocolVersion)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			status := response.StatusCode
			discard(response)
			if isDiscoveryMiss(status) {
				continue
			}
			return nil, &statusError{message: fmt.Sprintf("HTTP %d loading authorization server metadata from %s", status, candidate.URL)}
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := unmarshalJSON(body, &decoded); err != nil {
			return nil, err
		}
		metadata, err := ParseAuthorizationServerMetadata(decoded)
		if err != nil {
			return nil, err
		}
		if !options.SkipIssuerValidation {
			expected := authorizationServerURL
			// URL parsing adds a trailing slash to bare origins, so compare
			// without one on either side.
			trim := func(value string) string { return strings.TrimSuffix(value, "/") }
			if trim(metadata.Issuer) != trim(expected) {
				return nil, &OAuthIssuerMismatchError{Expected: expected, Received: &metadata.Issuer}
			}
		}
		return metadata, nil
	}
	return nil, nil
}

// DiscoverOAuthServerInfo discovers the protected resource and authorization
// server (upstream discoverOAuthServerInfo).
func DiscoverOAuthServerInfo(ctx context.Context, serverURL string, options DiscoveryOptions) (*OAuthServerInfo, error) {
	fetch := options.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	var resourceMetadata *OAuthProtectedResourceMetadata
	resource, err := DiscoverProtectedResourceMetadata(ctx, serverURL, DiscoveryOptions{
		ResourceMetadataURL: options.ResourceMetadataURL,
		Fetch:               fetch,
		ProtocolVersion:     options.ProtocolVersion,
	})
	if err == nil {
		resourceMetadata = resource
	} else if isNetworkError(err) {
		// Upstream rethrows only TypeErrors (network failures); status and
		// parse errors on the optional document are swallowed.
		return nil, err
	}
	if options.AuthorizationServerMetadataURL != nil {
		parsed, err := url.Parse(*options.AuthorizationServerMetadataURL)
		if err != nil {
			return nil, err
		}
		response, err := fetchMetadata(ctx, fetch, parsed, LatestProtocolVersion)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			status := response.StatusCode
			discard(response)
			return nil, &statusError{message: fmt.Sprintf("HTTP %d loading authorization server metadata from %s", status, parsed)}
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := unmarshalJSON(body, &decoded); err != nil {
			return nil, err
		}
		metadata, err := ParseAuthorizationServerMetadata(decoded)
		if err != nil {
			return nil, err
		}
		return &OAuthServerInfo{AuthorizationServerURL: metadata.Issuer, AuthorizationServerMetadata: metadata, ResourceMetadata: resourceMetadata}, nil
	}
	authorizationServerURL := serverURL
	if resourceMetadata != nil && len(resourceMetadata.AuthorizationServers) > 0 {
		authorizationServerURL = resourceMetadata.AuthorizationServers[0]
	} else {
		parsed, err := url.Parse(serverURL)
		if err != nil {
			return nil, err
		}
		root := *parsed
		root.Path = "/"
		root.RawQuery = ""
		root.ForceQuery = false
		root.Fragment = ""
		root.RawFragment = ""
		authorizationServerURL = root.String()
	}
	metadata, err := DiscoverAuthorizationServerMetadata(ctx, authorizationServerURL, DiscoveryOptions{
		Fetch:                fetch,
		SkipIssuerValidation: options.SkipIssuerValidation,
		ProtocolVersion:      options.ProtocolVersion,
	})
	if err != nil {
		return nil, err
	}
	return &OAuthServerInfo{AuthorizationServerURL: authorizationServerURL, AuthorizationServerMetadata: metadata, ResourceMetadata: resourceMetadata}, nil
}

// isNetworkError mirrors upstream's TypeError reservation: status and
// validation failures on the optional protected-resource document are
// swallowed, network failures are not.
func isNetworkError(err error) bool {
	var status *statusError
	if errors.As(err, &status) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

// ResourceURLFromServerURL strips the fragment (upstream
// resourceUrlFromServerUrl).
func ResourceURLFromServerURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	parsed.Fragment = ""
	return parsed.String()
}

// statusError is a typed HTTP-status failure; discovery reserves network
// errors (upstream TypeError) but swallows these.
type statusError struct {
	message string
}

func (e *statusError) Error() string { return e.message }

// SelectResource validates the metadata's resource against the MCP server
// URL (upstream selectResource).
func SelectResource(serverURL string, metadata *OAuthProtectedResourceMetadata) (*string, error) {
	if metadata == nil {
		return nil, nil
	}
	requested, err := url.Parse(ResourceURLFromServerURL(serverURL))
	if err != nil {
		return nil, err
	}
	configured, err := url.Parse(metadata.Resource)
	if err != nil {
		return nil, fmt.Errorf("Protected resource %s does not match MCP server %s", metadata.Resource, requested)
	}
	if originOf(requested) != originOf(configured) {
		return nil, fmt.Errorf("Protected resource %s does not match MCP server %s", metadata.Resource, requested)
	}
	requestedPath := ensureSlash(requested.EscapedPath())
	configuredPath := ensureSlash(configured.EscapedPath())
	if !strings.HasPrefix(requestedPath, configuredPath) {
		return nil, fmt.Errorf("Protected resource %s does not match MCP server %s", metadata.Resource, requested)
	}
	return &metadata.Resource, nil
}

func ensureSlash(path string) string {
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

// originOf renders a URL's origin the way the WHATWG URL parser does: the
// lowercased scheme and host, with a default port dropped.
func originOf(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	if scheme == "http" {
		host = strings.TrimSuffix(host, ":80")
	} else if scheme == "https" {
		host = strings.TrimSuffix(host, ":443")
	}
	return scheme + "://" + host
}
