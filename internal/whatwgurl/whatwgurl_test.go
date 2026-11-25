package whatwgurl

import (
	"net/url"
	"testing"
)

// `new URL(" \thttps://ex\nample.com/a b\r\n ")` reads https://example.com/a%20b:
// the ends lose C0 controls and spaces, and tab, CR and LF go everywhere.
func TestStripWhitespaceMatchesTheParserPreprocessing(t *testing.T) {
	for in, want := range map[string]string{
		" \thttps://ex\nample.com/a b\r\n ": "https://example.com/a b",
		"\x00\x1fhttp://x/\x01":             "http://x/",
		"https://example.com":               "https://example.com",
		"":                                  "",
	} {
		if got := StripWhitespace(in); got != want {
			t.Errorf("StripWhitespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOriginNormalizes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://LOCALHOST/x", "http://localhost"},
		{"https://Example.com:443/y", "https://example.com"},
		{"http://example.com:80/z", "http://example.com"},
		{"https://example.com:8443/z", "https://example.com:8443"},
	} {
		u, err := url.Parse(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if got := Origin(u); got != tc.want {
			t.Errorf("Origin(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ResolvePath applies the URL parser's path state. Node's URL gives these exact
// answers for http://h<path>, including the trailing-slash and repeated-slash
// cases that a naive segment filter gets wrong.
func TestResolvePathMatchesTheURLParser(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/auth/callback", "/auth/callback"},
		{"/auth/../auth/callback", "/auth/callback"},
		{"/auth/./callback", "/auth/callback"},
		{"/a/b/../..", "/"},
		{"/a/b/../../..", "/"},
		{"/../x", "/x"},
		{"/a//b", "/a//b"},
		{"/a/./", "/a/"},
		{"/a/../", "/"},
		{"/", "/"},
		{"/a/b/c/./../../d", "/a/d"},
	} {
		if got := ResolvePath(tc.path); got != tc.want {
			t.Errorf("ResolvePath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
