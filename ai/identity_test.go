package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// Upstream v1.4.0 adc5fa9f8/04a93bad8 (utils/identity.ts): requests identify
// pi with a versioned detailed user agent unless a short or custom form is
// selected.
func TestFormatUserAgent(t *testing.T) {
	detailed := FormatUserAgent(nil)
	if !strings.HasPrefix(detailed, "pi/"+AIVersion+" ("+runtime.GOOS+"; go/") ||
		!strings.HasSuffix(detailed, "; "+runtime.GOARCH+")") {
		t.Fatalf("detailed user agent = %q", detailed)
	}
	versioned := FormatUserAgent(&ClientIdentity{Version: "9.9.9"})
	if !strings.HasPrefix(versioned, "pi/9.9.9 (") {
		t.Fatalf("versioned user agent = %q", versioned)
	}
	if got := FormatUserAgent(&ClientIdentity{UserAgent: "short"}); got != "pi" {
		t.Fatalf("short user agent = %q", got)
	}
	if got := FormatUserAgent(&ClientIdentity{UserAgent: "acme/1.0"}); got != "acme/1.0" {
		t.Fatalf("custom user agent = %q", got)
	}
}

func TestLiteralUserAgent(t *testing.T) {
	// Reference: pi v1.4.0 utils/identity.ts, {custom} is distinct from
	// the short and detailed modes, even when the literal equals a mode.
	for _, literal := range []string{"", "short", "detailed", "acme/2.0"} {
		if got := FormatUserAgent(&ClientIdentity{CustomUserAgent: &literal}); got != literal {
			t.Fatalf("literal %q: got %q", literal, got)
		}
	}
}

func TestAttributionHeaders(t *testing.T) {
	pi := &ClientAttribution{Name: "pi", URL: "https://pi.dev", Categories: []string{"cli-agent"}}
	cases := []struct {
		name   string
		model  *Model
		want   map[string]string
		absent bool
	}{
		{"openrouter", &Model{Provider: "openrouter"}, map[string]string{
			"HTTP-Referer": "https://pi.dev", "X-OpenRouter-Title": "pi", "X-OpenRouter-Categories": "cli-agent",
		}, false},
		{"openrouter by host", &Model{Provider: "custom", BaseURL: "https://openrouter.ai/api/v1"}, map[string]string{
			"HTTP-Referer": "https://pi.dev", "X-OpenRouter-Title": "pi", "X-OpenRouter-Categories": "cli-agent",
		}, false},
		{"nvidia capitalizes", &Model{Provider: "nvidia"}, map[string]string{"X-BILLING-INVOKE-ORIGIN": "Pi"}, false},
		{"opencode", &Model{Provider: "opencode"}, map[string]string{"x-opencode-client": "pi"}, false},
		{"cloudflare has none", &Model{Provider: "cloudflare-workers-ai"}, nil, true},
		{"unrelated", &Model{Provider: "anthropic"}, nil, true},
	}
	for _, tc := range cases {
		got := AttributionHeaders(tc.model, &ClientIdentity{Attribution: pi})
		if tc.absent {
			if len(got) != 0 {
				t.Fatalf("%s: headers = %v", tc.name, got)
			}
			continue
		}
		for key, want := range tc.want {
			if got[key] != want {
				t.Fatalf("%s: %s = %q, want %q (all %v)", tc.name, key, got[key], want, got)
			}
		}
	}
	if got := AttributionHeaders(&Model{Provider: "openrouter"}, nil); len(got) != 0 {
		t.Fatalf("nil attribution headers = %v", got)
	}
}

func TestAttributionCapitalizesFirstUTF16Unit(t *testing.T) {
	// Reference: pi v1.4.0 utils/identity.ts, charAt(0).toUpperCase().
	for _, tc := range []struct{ name, want string }{
		{"ßclient", "SSclient"}, {"éclient", "Éclient"}, {"𐐨client", "𐐨client"}, {"", ""},
	} {
		got := AttributionHeaders(&Model{Provider: "nvidia"}, &ClientIdentity{
			Attribution: &ClientAttribution{Name: tc.name},
		})
		if got["X-BILLING-INVOKE-ORIGIN"] != tc.want {
			t.Fatalf("name %q: headers = %v, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOpenCodeSessionHeaders(t *testing.T) {
	model := &Model{Provider: "opencode"}
	if got := OpenCodeSessionHeaders(model, "sess-1"); got["x-opencode-session"] != "sess-1" {
		t.Fatalf("session headers = %v", got)
	}
	if got := OpenCodeSessionHeaders(model, ""); got != nil {
		t.Fatalf("empty session headers = %v", got)
	}
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://OPENCODE.AI/zen/v1", true},
		{"https://opencode.ai.attacker.invalid", false},
		{"//opencode.ai/zen/v1", false},
	} {
		if got := OpenCodeSessionHeaders(&Model{Provider: "custom", BaseURL: tc.url}, "s"); (got != nil) != tc.want {
			t.Fatalf("target %q: headers = %v", tc.url, got)
		}
	}
	if got := OpenCodeSessionHeaders(&Model{Provider: "opencode-go"}, "s"); got["x-opencode-session"] != "s" {
		t.Fatalf("opencode-go headers = %v", got)
	}
	if got := OpenCodeSessionHeaders(&Model{Provider: "anthropic"}, "sess-1"); got != nil {
		t.Fatalf("unrelated session headers = %v", got)
	}
}

type identityImages struct{ capture func(*ClientIdentity) }

func (s identityImages) GenerateImages(_ *ImagesModel, _ ImagesContext, options *ImagesOptions) *AssistantImages {
	s.capture(options.Identity)
	return &AssistantImages{StopReason: ImagesStopStop}
}

// Reference: pi v1.4.0 models.ts generateImages/applyAuth. This port keeps
// image providers in a separate registry but shares CreateModelsOptions.
func TestImagesModelsDefaultIdentity(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		defaults := &ClientIdentity{UserAgent: "short", Attribution: PIAttribution}
		models := CreateImagesModels(&CreateModelsOptions{Identity: defaults})
		model := &ImagesModel{ID: "m", Provider: "p", API: KnownImagesApiOpenRouter}
		var captured *ClientIdentity
		auth := ProviderAuth{}
		if resolved {
			auth.APIKey = &ApiKeyAuth{Name: "p", Resolve: func(AuthResolveInput) (*AuthResult, error) {
				return &AuthResult{Auth: ModelAuth{APIKey: "key"}}, nil
			}}
		}
		models.SetProvider(CreateImagesProvider(ImageProviderOptions{
			ID: "p", Models: []*ImagesModel{model}, Auth: auth,
			API: identityImages{capture: func(identity *ClientIdentity) { captured = identity }},
		}))
		models.GenerateImages(model, ImagesContext{}, nil)
		if captured == nil || captured.UserAgent != "short" || captured.Attribution != PIAttribution {
			t.Fatalf("resolved %t: default identity = %+v", resolved, captured)
		}
		request := &ClientIdentity{AttributionSet: true}
		models.GenerateImages(model, ImagesContext{}, &ImagesOptions{Identity: request})
		if captured == nil || captured.UserAgent != "short" || captured.Attribution != nil || !captured.AttributionSet {
			t.Fatalf("resolved %t: request identity = %+v", resolved, captured)
		}
		if defaults.Attribution != PIAttribution || request.UserAgent != "" {
			t.Fatal("image request changed caller-owned identities")
		}
	}
}

// Reference: pi v1.4.0 models.ts applyAuth (04a93bad8). Auth may change
// the target host; session defaults precede auth and request headers, and
// TransformHeaders sees the result exactly once.
func TestResolvedAuthOpenCodeSessionHeaders(t *testing.T) {
	model := &Model{Provider: "custom", BaseURL: "https://example.com"}
	auth := ModelAuth{BaseURL: "https://opencode.ai/zen/v1"}
	for _, tc := range []struct {
		name           string
		authHeaders    ProviderHeaders
		requestHeaders ProviderHeaders
		want           *string
	}{
		{"session default", nil, nil, new("session-1")},
		{"auth override", ProviderHeaders{"X-OpenCode-Session": new("auth")}, nil, new("auth")},
		{"caller override", ProviderHeaders{"X-OpenCode-Session": new("auth")}, ProviderHeaders{"x-opencode-session": new("caller")}, new("caller")},
		{"caller removal", nil, ProviderHeaders{"X-OpenCode-Session": nil}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth.Headers = tc.authHeaders
			calls := 0
			requestModel, options := ApplyResolvedAuth(model, auth, nil, StreamOptions{
				SessionID: "session-1", Headers: tc.requestHeaders,
				TransformHeaders: func(headers ProviderHeaders) ProviderHeaders {
					calls++
					found := false
					for name, value := range headers {
						if lowerName(name) != "x-opencode-session" {
							continue
						}
						if found {
							t.Fatal("duplicate session header")
						}
						found = true
						if (value == nil) != (tc.want == nil) || value != nil && *value != *tc.want {
							t.Fatalf("session header = %v; want %v", value, tc.want)
						}
					}
					if !found {
						t.Fatal("session header missing")
					}
					return headers
				},
			})
			if requestModel.BaseURL != auth.BaseURL || model.BaseURL != "https://example.com" || calls != 1 || options.TransformHeaders != nil {
				t.Fatalf("model = %+v, transform calls = %d", requestModel, calls)
			}
		})
	}
}

// Reference: pi v1.4.0 api/openrouter-images.ts applies identity defaults
// beneath model and caller headers, including null removals.
func TestImageRequestIdentityHeaders(t *testing.T) {
	for _, mode := range []string{"identity", "model override", "request override", "remove"} {
		t.Run(mode, func(t *testing.T) {
			captured := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"header probe"}}`))
			}))
			defer server.Close()
			model := &ImagesModel{ID: "image-model", API: KnownImagesApiOpenRouter, Provider: "openrouter", BaseURL: server.URL}
			options := &ImagesOptions{
				APIKey: "token", Identity: &ClientIdentity{UserAgent: "image-agent", Attribution: PIAttribution}}
			wantAgent, wantTitle := "image-agent", "pi"
			if mode != "identity" {
				model.Headers = map[string]string{"User-Agent": "model-agent", "X-OpenRouter-Title": "model-title"}
				wantAgent, wantTitle = "model-agent", "model-title"
			}
			if mode == "request override" {
				options.Headers = ProviderHeaders{"user-agent": new("request-agent"), "x-openrouter-title": new("request-title")}
				wantAgent, wantTitle = "request-agent", "request-title"
			}
			if mode == "remove" {
				options.Headers = ProviderHeaders{"user-agent": nil, "x-openrouter-title": nil}
				wantAgent, wantTitle = "", ""
			}
			output := (OpenRouterImages{}).GenerateImages(model, ImagesContext{Input: []Content{TextContent{Text: "cat"}}}, options)
			if output.StopReason != ImagesStopError {
				t.Fatalf("probe result = %+v", output)
			}
			select {
			case headers := <-captured:
				if headers.Get("User-Agent") != wantAgent || headers.Get("X-OpenRouter-Title") != wantTitle {
					t.Fatalf("headers = %v; want agent %q, title %q", headers, wantAgent, wantTitle)
				}
			default:
				t.Fatalf("no HTTP request; output = %+v", output)
			}
		})
	}
}

// The reference resolves the identity version with `identity?.version ??
// PI_AI_VERSION` (pi v1.5.0 f9a26365 utils/identity.ts:63), so a JSON null
// reads as absent and the default version is used. The port's decoder marked
// any present member as explicit, including null, and rendered "pi/ (..." with
// no version at all (D431).
func TestIdentityNullVersionUsesTheDefault(t *testing.T) {
	var nullVersion ClientIdentity
	if err := json.Unmarshal([]byte(`{"version":null}`), &nullVersion); err != nil {
		t.Fatal(err)
	}
	if nullVersion.VersionSet {
		t.Fatal(`{"version":null} must not read as an explicit version`)
	}
	if got := FormatUserAgent(&nullVersion); got != FormatUserAgent(&ClientIdentity{}) {
		t.Fatalf(`null version rendered %q, want the default identity's header`, got)
	}

	// An empty string is still explicit, as it is upstream.
	var emptyVersion ClientIdentity
	if err := json.Unmarshal([]byte(`{"version":""}`), &emptyVersion); err != nil {
		t.Fatal(err)
	}
	if !emptyVersion.VersionSet {
		t.Fatal(`{"version":""} must stay an explicit empty version`)
	}
	if ua := FormatUserAgent(&emptyVersion); !strings.HasPrefix(ua, "pi/ (") {
		t.Fatalf(`empty version rendered %q`, ua)
	}

	// A real version still wins.
	var setVersion ClientIdentity
	if err := json.Unmarshal([]byte(`{"version":"9.9.9"}`), &setVersion); err != nil {
		t.Fatal(err)
	}
	if ua := FormatUserAgent(&setVersion); !strings.HasPrefix(ua, "pi/9.9.9 (") {
		t.Fatalf("set version rendered %q", ua)
	}
}

// identity.ts:82-88 matchesHost parses baseUrl with new URL and compares hostname, so
// a lookalike that merely CONTAINS the expected host must not match: the attribution
// headers would otherwise be attached to a third-party endpoint. Verified against the
// reference's own matchesHost for all eight shapes.
func TestMatchesBaseURLHostRejectsLookalikes(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://integrate.api.nvidia.com/v1", true},
		{"https://INTEGRATE.API.NVIDIA.COM/v1", true},
		{"http://integrate.api.nvidia.com", true},
		// No scheme: the reference's new URL() throws, so it is not a match even
		// though the string is exactly the host.
		{"integrate.api.nvidia.com", false},
		{"https://x.com/integrate.api.nvidia.com", false},
		{"https://integrate.api.nvidia.com.evil.com", false},
		{"", false},
		{"://bad", false},
	} {
		if got := matchesBaseURLHost(tc.baseURL, "integrate.api.nvidia.com"); got != tc.want {
			t.Errorf("matchesBaseURLHost(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}
