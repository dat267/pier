package ai

import (
	"runtime"
	"strings"
	"testing"
)

// Upstream v1.4.0 adc5fa9f8/04a93bad8 (utils/identity.ts): requests identify
// pi with a versioned detailed user agent unless a short or custom form is
// selected.
func TestFormatUserAgent(t *testing.T) {
	detailed := FormatUserAgent(nil)
	if !strings.HasPrefix(detailed, "pi/1.4.0 ("+runtime.GOOS+"; go/") ||
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
