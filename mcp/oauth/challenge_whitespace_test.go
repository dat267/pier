package oauth

import "testing"

// pi v1.4.0 discovery.ts uses RegExp(..., "i") without the Unicode flag:
// the long s must not match the ASCII s in the parameter name "scope".
func TestWwwAuthenticateDoesNotFoldUnicodeParameterNames(t *testing.T) {
	got := ParseWwwAuthenticate(`Bearer ſcope="wrong", scope="files:read"`)
	if got.Scope == nil || *got.Scope != "files:read" {
		t.Fatalf("scope = %v, want the ASCII scope parameter", got.Scope)
	}
}

// pi v1.4.0 packages/mcp/src/oauth/discovery.ts field uses JavaScript \s for
// both the parameter delimiter and the end of an unquoted parameter value.
func TestWwwAuthenticateParametersUseJSWhitespace(t *testing.T) {
	for _, space := range []string{"\v", "\u00a0", "\u1680", "\u2003", "\u2028", "\u202f", "\u3000", "\ufeff"} {
		header := "Bearer" + space + "scope=files:read" + space + "error=invalid_token"
		got := ParseWwwAuthenticate(header)
		if got.Scope == nil || *got.Scope != "files:read" || got.Error == nil || *got.Error != "invalid_token" {
			t.Errorf("space %q: challenge = %+v, want separate scope and error parameters", space, got)
		}
	}
	// Whitespace inside a quoted value is content, not a delimiter to normalize.
	got := ParseWwwAuthenticate("Bearer\u00a0scope=\"files:read\u00a0files:write\"")
	if got.Scope == nil || *got.Scope != "files:read\u00a0files:write" {
		t.Fatalf("quoted scope did not retain its whitespace: %+v", got)
	}
}

// Reference: pi v1.6.0 oauth/discovery.ts parseWwwAuthenticate resolves the
// parameter with `new URL(resourceMetadata)`, so the stored value is the WHATWG
// serialization: the host lowercased and the path percent-encoded. pier kept the
// header's raw spelling, so a server advertising `HTTP://X.TEST/md` or a path
// with a space reached the follow-up discovery request as a different URL string
// than the reference used.
func TestWwwAuthenticateResourceMetadataIsWHATWGSerialized(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{`Bearer resource_metadata="https://x.test/md"`, "https://x.test/md"},
		{`Bearer resource_metadata="HTTP://X.TEST/md"`, "http://x.test/md"},
		{`Bearer resource_metadata="https://x.test/a b"`, "https://x.test/a%20b"},
		// Not resolvable against no base: both sides drop these.
		{`Bearer resource_metadata="x.test/md"`, ""},
		{`Bearer resource_metadata="/relative/md"`, ""},
	} {
		got := ParseWwwAuthenticate(tc.header).ResourceMetadataURL
		if tc.want == "" {
			if got != nil {
				t.Fatalf("%q: kept %q, want none", tc.header, *got)
			}
			continue
		}
		if got == nil || *got != tc.want {
			t.Fatalf("%q: got %v, want %q", tc.header, got, tc.want)
		}
	}
}
