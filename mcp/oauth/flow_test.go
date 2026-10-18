package oauth

import "testing"

// secureEndpoint must treat loopback hosts case-insensitively, like the WHATWG
// URL parser's lowercased hostname: HTTP is allowed on loopback only.
func TestSecureEndpointLoopbackIsCaseInsensitive(t *testing.T) {
	for _, value := range []string{"http://localhost:8080/x", "http://LOCALHOST/x", "http://127.0.0.1/x", "http://[::1]/x"} {
		if _, err := secureEndpoint(value); err != nil {
			t.Errorf("secureEndpoint(%q) = %v, want allowed", value, err)
		}
	}
	if _, err := secureEndpoint("http://example.com/x"); err == nil {
		t.Error("secureEndpoint(http://example.com) accepted a non-loopback HTTP URL")
	}
}
