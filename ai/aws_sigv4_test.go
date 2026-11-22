package ai

import (
	"net/url"
	"testing"
)

// AWS SigV4's canonical query is built from the raw query string: each name and
// value is URI-encoded with RFC 3986 rules, so a literal '+' is a plus and encodes
// to %2B, never a space. `url.ParseQuery` follows the HTML form rule and decodes
// '+' as a space, which re-encodes to %20 and would change the signature. The
// reference has no SigV4 code (Bedrock goes through the AWS SDK), so the
// specification is the oracle here.
func TestCanonicalizeAWSQueryTreatsPlusAsALiteral(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"a=x+y", "a=x%2By"},
		{"a=+", "a=%2B"},
		{"a=%2B", "a=%2B"},
		{"a=x%20y", "a=x%20y"},
		{"a=1&b=2", "a=1&b=2"},
		{"b=2&a=1", "a=1&b=2"},
		{"a=2&a=1", "a=1&a=2"},
		{"a", "a="},
		{"a=1&", "a=1"},
	} {
		requestURL := &url.URL{Scheme: "https", Host: "x", RawQuery: tc.raw}
		if got := canonicalizeAWSQuery(requestURL); got != tc.want {
			t.Errorf("canonicalizeAWSQuery(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
