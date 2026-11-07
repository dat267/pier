package oauth

import "testing"

// D276: pi v1.5.0 (f9a26365) packages/mcp/src/oauth/discovery.ts
// resourceUrlFromServerUrl returns a WHATWG URL, which drops the default port
// and lowercases the host (`https://example.com:443/mcp` serializes as
// `https://example.com/mcp`). The Go helper keeps both, so the parse golden
// excludes these outputs. What upstream decides from them, SelectResource's
// origin and path match, must still agree.
func TestResourceURLKeepsDefaultPortAndHostCase(t *testing.T) {
	for _, test := range []struct{ server, want, upstream string }{
		{"https://example.com:443/mcp", "https://example.com:443/mcp", "https://example.com/mcp"},
		{"https://EXAMPLE.COM/MCP#frag", "https://EXAMPLE.COM/MCP", "https://example.com/MCP"},
	} {
		got := ResourceURLFromServerURL(test.server)
		if got != test.want {
			t.Errorf("ResourceURLFromServerURL(%q) = %q, want %q (upstream %q)", test.server, got, test.want, test.upstream)
		}
		// Upstream's selectResource matches the normalized form against the
		// metadata; the port's spelling and the host case must not change that.
		resource := "https://example.com/"
		selected, err := SelectResource(test.server, &OAuthProtectedResourceMetadata{Resource: resource})
		if err != nil || selected == nil || *selected != resource {
			t.Errorf("SelectResource(%q) = %v, %v; want %q as upstream", test.server, selected, err, resource)
		}
	}
}
