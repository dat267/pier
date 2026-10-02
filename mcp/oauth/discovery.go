package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/dat267/pier/mcp/protocol"
)

// McpFetch performs HTTP requests (upstream's fetch injection).
type McpFetch func(ctx context.Context, request *http.Request) (*http.Response, error)

// defaultFetch is the platform client, called without a receiver (#10188).
func defaultFetch(ctx context.Context, request *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(request)
}

// LatestProtocolVersion re-exports the MCP version for metadata requests.
const LatestProtocolVersion = protocol.LatestProtocolVersion

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

// field pulls one auth-param out of a challenge header; an empty value
// (`scope=""`) carries no information and counts as absent.
func field(header, name string) *string {
	re, err := regexp.Compile(`(?i)(?:^|[,\s])` + regexp.QuoteMeta(name) + `=(?:"([^"]*)"|([^\s,]+))`)
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
	scheme := strings.ToLower(strings.Fields(strings.TrimSpace(header))[0])
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
	target := *server
	if options.ResourceMetadataURL != nil {
		configured, err := url.Parse(*options.ResourceMetadataURL)
		if err != nil {
			return nil, err
		}
		target = *configured
	} else if suffix := pathSuffix(server.Path); suffix != "" {
		// Upstream joins the well-known path onto the origin, replacing the
		// server path.
		target.Path = "/.well-known/oauth-protected-resource" + suffix
	} else {
		target.Path = "/.well-known/oauth-protected-resource"
	}
	response, err := fetchMetadata(ctx, fetch, &target, version)
	if err != nil {
		return nil, err
	}
	if options.ResourceMetadataURL == nil && server.Path != "/" && isDiscoveryMiss(response.StatusCode) {
		discard(response)
		fallback := *server
		fallback.Path = "/.well-known/oauth-protected-resource"
		response, err = fetchMetadata(ctx, fetch, &fallback, version)
		if err != nil {
			return nil, err
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := response.StatusCode
		discard(response)
		return nil, &statusError{status: status, message: fmt.Sprintf("HTTP %d loading OAuth protected resource metadata", status)}
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
	path := pathSuffix(issuer.Path)
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
	clone := base
	clone.Path = suffix
	return clone.String()
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
			return nil, &statusError{status: status, message: fmt.Sprintf("HTTP %d loading authorization server metadata from %s", status, candidate.URL)}
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
			return nil, &statusError{status: status, message: fmt.Sprintf("HTTP %d loading authorization server metadata from %s", status, parsed)}
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
	return !strings.Contains(err.Error(), "Invalid ")
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
	status  int
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
	if requested.Scheme != configured.Scheme || requested.Host != configured.Host {
		return nil, fmt.Errorf("Protected resource %s does not match MCP server %s", metadata.Resource, requested)
	}
	requestedPath := ensureSlash(requested.Path)
	configuredPath := ensureSlash(configured.Path)
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
