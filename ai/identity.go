package ai

import (
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// AIVersion is the pi-ai package version in the detailed user agent. Upstream
// keeps it in sync with package.json (utils/version.ts); the port pins the
// reference release here.
const AIVersion = "1.4.0"

// ClientAttribution is who requests are attributed to. Name and URL are
// required upstream.
type ClientAttribution struct {
	Name       string
	URL        string
	Categories []string
}

// PIAttribution is pi's own provider attribution.
var PIAttribution = &ClientAttribution{Name: "pi", URL: "https://pi.dev", Categories: []string{"cli-agent"}}

// ClientIdentity identifies the calling app to providers (upstream
// utils/identity.ts ClientIdentity). The zero value sends the detailed
// user agent with AIVersion and no attribution headers.
type ClientIdentity struct {
	// UserAgent is "detailed" (default), "short", or a custom literal value.
	UserAgent string
	// CustomUserAgent is a literal value, including "", "short", or
	// "detailed". When set it wins over UserAgent (upstream { custom }).
	CustomUserAgent *string
	// Version overrides the version in the detailed user agent when non-empty.
	Version string
	// Attribution adds provider attribution headers; nil adds none.
	Attribution *ClientAttribution
}

// FormatUserAgent renders the User-Agent of an identity. The detailed form is
// pi/<version> (<platform>; <runtime>/<version>; <arch>).
// D-row D265: Linux/amd64 reports go/<version> and amd64, not node/v<version>
// and x64. This identifies the actual runtime and Go platform names.
func FormatUserAgent(identity *ClientIdentity) string {
	mode := ""
	version := AIVersion
	if identity != nil {
		if identity.CustomUserAgent != nil {
			return *identity.CustomUserAgent
		}
		mode = identity.UserAgent
		if identity.Version != "" {
			version = identity.Version
		}
	}
	switch mode {
	case "", "detailed":
		return "pi/" + version + " (" + runtime.GOOS + "; go/" +
			strings.TrimPrefix(runtime.Version(), "go") + "; " + runtime.GOARCH + ")"
	case "short":
		return "pi"
	default:
		return mode
	}
}

func matchesBaseURLHost(baseURL, host string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), host)
}

func isOpenRouterTarget(provider ProviderId, baseURL string) bool {
	return provider == "openrouter" || strings.Contains(baseURL, "openrouter.ai")
}

func isNvidiaNIMTarget(provider ProviderId, baseURL string) bool {
	return provider == "nvidia" || matchesBaseURLHost(baseURL, "integrate.api.nvidia.com")
}

func isOpenCodeTarget(provider ProviderId, baseURL string) bool {
	return provider == "opencode" || provider == "opencode-go" ||
		matchesBaseURLHost(baseURL, "opencode.ai")
}

// AttributionHeaders maps an identity's attribution to the provider's headers
// (upstream getAttributionHeaders). Attribution off returns an empty map.
func AttributionHeaders(model *Model, identity *ClientIdentity) map[string]string {
	if model == nil {
		return nil
	}
	return attributionHeaders(model.Provider, model.BaseURL, identity)
}

func attributionHeaders(provider ProviderId, baseURL string, identity *ClientIdentity) map[string]string {
	if identity == nil || identity.Attribution == nil {
		return nil
	}
	attribution := identity.Attribution
	switch {
	case isOpenRouterTarget(provider, baseURL):
		headers := map[string]string{
			"HTTP-Referer":       attribution.URL,
			"X-OpenRouter-Title": attribution.Name,
		}
		if len(attribution.Categories) > 0 {
			headers["X-OpenRouter-Categories"] = strings.Join(attribution.Categories, ",")
		}
		return headers
	case isNvidiaNIMTarget(provider, baseURL):
		name := attribution.Name
		// charAt(0) selects one UTF-16 unit, so a supplementary letter's
		// leading surrogate is not capitalized. BMP casing can expand.
		if first, size := utf8.DecodeRuneInString(name); size > 0 && first <= 0xffff {
			name = cases.Upper(language.Und).String(name[:size]) + name[size:]
		}
		return map[string]string{"X-BILLING-INVOKE-ORIGIN": name}
	case isOpenCodeTarget(provider, baseURL):
		return map[string]string{"x-opencode-client": attribution.Name}
	default:
		return nil
	}
}

// OpenCodeSessionHeaders returns the per-conversation routing header for
// OpenCode, including custom providers pointing at it. Unlike attribution it
// does not depend on telemetry.
func OpenCodeSessionHeaders(model *Model, sessionID string) map[string]string {
	if model == nil || sessionID == "" || !isOpenCodeTarget(model.Provider, model.BaseURL) {
		return nil
	}
	return map[string]string{"x-opencode-session": sessionID}
}

// IdentityHeaders are the default headers an identity adds to a request:
// User-Agent plus provider attribution. Provider, model and request headers
// are applied after these, so they can override or remove them.
func IdentityHeaders(model *Model, identity *ClientIdentity) map[string]string {
	if model == nil {
		return map[string]string{"User-Agent": FormatUserAgent(identity)}
	}
	return identityHeaders(model.Provider, model.BaseURL, identity)
}

func identityHeaders(provider ProviderId, baseURL string, identity *ClientIdentity) map[string]string {
	headers := map[string]string{"User-Agent": FormatUserAgent(identity)}
	for key, value := range attributionHeaders(provider, baseURL, identity) {
		headers[key] = value
	}
	return headers
}

// ImagesIdentityHeaders is IdentityHeaders for an image-generation target.
func ImagesIdentityHeaders(model *ImagesModel, identity *ClientIdentity) map[string]string {
	if model == nil {
		return map[string]string{"User-Agent": FormatUserAgent(identity)}
	}
	return identityHeaders(model.Provider, model.BaseURL, identity)
}

// applyProviderHeaders applies caller overrides after defaults. An empty
// User-Agent suppresses Go's transport default; deleting it would send
// Go-http-client/1.1 instead of honoring the caller's null override.
func applyProviderHeaders(header http.Header, overrides ProviderHeaders) {
	for name, value := range overrides {
		if value == nil {
			if strings.EqualFold(name, "User-Agent") {
				header.Set(name, "")
			} else {
				header.Del(name)
			}
			continue
		}
		header.Set(name, *value)
	}
}

// ApplyIdentityHeaders installs the identity defaults on an HTTP request.
func ApplyIdentityHeaders(header http.Header, model *Model, identity *ClientIdentity) {
	for key, value := range IdentityHeaders(model, identity) {
		header.Set(key, value)
	}
}
