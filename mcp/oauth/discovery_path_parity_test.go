package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/discovery.ts.
// `new URL` removes dot segments from the path (including the `.`/`%2e` forms)
// before pathSuffix and the well-known candidates use it.
func TestBuildAuthorizationServerDiscoveryUrlsNormalizesDotSegments(t *testing.T) {
	cases := []struct {
		issuer string
		want   []string
	}{
		{
			"https://h/a/../b",
			[]string{
				"https://h/.well-known/oauth-authorization-server/b",
				"https://h/.well-known/openid-configuration/b",
				"https://h/b/.well-known/openid-configuration",
			},
		},
		{
			"https://h/./x",
			[]string{
				"https://h/.well-known/oauth-authorization-server/x",
				"https://h/.well-known/openid-configuration/x",
				"https://h/x/.well-known/openid-configuration",
			},
		},
		{
			"https://h/%2e%2e/y",
			[]string{
				"https://h/.well-known/oauth-authorization-server/y",
				"https://h/.well-known/openid-configuration/y",
				"https://h/y/.well-known/openid-configuration",
			},
		},
		{
			"https://h/a/%2e/b",
			[]string{
				"https://h/.well-known/oauth-authorization-server/a/b",
				"https://h/.well-known/openid-configuration/a/b",
				"https://h/a/b/.well-known/openid-configuration",
			},
		},
		{
			"https://h/a/%2e./b",
			[]string{
				"https://h/.well-known/oauth-authorization-server/b",
				"https://h/.well-known/openid-configuration/b",
				"https://h/b/.well-known/openid-configuration",
			},
		},
		{
			"https://h/a/..",
			[]string{
				"https://h/.well-known/oauth-authorization-server",
				"https://h/.well-known/openid-configuration",
			},
		},
		{
			"https://h/a//../b",
			[]string{
				"https://h/.well-known/oauth-authorization-server/a/b",
				"https://h/.well-known/openid-configuration/a/b",
				"https://h/a/b/.well-known/openid-configuration",
			},
		},
		{
			// Intentional difference (ARCHITECTURE, M32): the reference's
			// `new URL("//x/.well-known/...", origin)` treats //x as another
			// host; pier keeps every candidate on the issuer's origin.
			"https://h//x",
			[]string{
				"https://h/.well-known/oauth-authorization-server//x",
				"https://h/.well-known/openid-configuration//x",
				"https://h//x/.well-known/openid-configuration",
			},
		},
	}
	for _, testCase := range cases {
		var got []string
		for _, candidate := range BuildAuthorizationServerDiscoveryUrls(testCase.issuer) {
			got = append(got, candidate.URL)
		}
		if strings.Join(got, "\n") != strings.Join(testCase.want, "\n") {
			t.Errorf("BuildAuthorizationServerDiscoveryUrls(%q):\n%v\nwant:\n%v", testCase.issuer, got, testCase.want)
		}
	}
}

// The reference compares normalized WHATWG paths, so an issuer reached through
// dot segments still matches the resource it advertises.
func TestSelectResourceComparesNormalizedPaths(t *testing.T) {
	resource := "https://h/b/mcp"
	matched, err := SelectResource("https://h/a/../b/mcp", &OAuthProtectedResourceMetadata{Resource: resource})
	if err != nil || matched == nil || *matched != resource {
		t.Fatalf("SelectResource = %v/%v, want %q", matched, err, resource)
	}
	if _, err := SelectResource("https://h/a/../b/mcp", &OAuthProtectedResourceMetadata{Resource: "https://h/other/mcp"}); err == nil {
		t.Fatal("a different normalized path matched")
	}
}

// End-to-end: the reference requests only the normalized well-known URL.
func TestDiscoverAuthorizationServerMetadataNormalizesDotSegments(t *testing.T) {
	var requested []string
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested = append(requested, request.URL.Path)
		if request.URL.Path != "/.well-known/oauth-authorization-server/b" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer":                   issuer,
			"authorization_endpoint":   issuer + "/authorize",
			"token_endpoint":           issuer + "/token",
			"response_types_supported": []string{"code"},
		})
	}))
	defer server.Close()
	issuer = server.URL + "/a/../b"

	metadata, err := DiscoverAuthorizationServerMetadata(context.Background(), issuer, DiscoveryOptions{})
	if err != nil || metadata == nil {
		t.Fatalf("discovery failed: metadata=%v err=%v requested=%v", metadata, err, requested)
	}
	if strings.Join(requested, "\n") != "/.well-known/oauth-authorization-server/b" {
		t.Fatalf("requested %v, want only the normalized candidate", requested)
	}
}
