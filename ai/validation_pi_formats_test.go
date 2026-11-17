package ai

import "testing"

// The expectations are the pinned reference's own answers: packages/schema/src/
// formats/index.ts (TypeBox 1.3.27) run under node against every case in the
// table, which is the authority ai.validateToolArguments defers to when a
// schema carries a `format`.
func TestPiFormatValidMatchesTheReferenceFormats(t *testing.T) {
	cases := []struct {
		format string
		value  string
		want   bool
	}{
		{"date", "2023-01-01", true},
		{"date", "+123-01-01", false},
		{"date", "2023-02-29", false},
		{"date", "2024-02-29", true},
		{"date", "2023-13-01", false},
		{"date", "2023-00-10", false},
		{"date", "2023-01-32", false},
		{"date", "2023-1-01", false},
		{"time", "23:59:60Z", true},
		{"time", "23:59:60+00:00", true},
		{"time", "12:00:60Z", false},
		{"time", "23:59:60", false},
		{"time", "24:00:00Z", false},
		{"time", "12:34:56.789Z", true},
		{"url", "https://example.com/x", true},
		{"url", "http://", false},
		{"url", "mailto:a@b", true},
		{"url", "a:b", true},
		{"url", "urn:isbn:1", true},
		{"url", "/relative", false},
		{"url", "https://例子.测试/", true},
		{"url", "ht tp://x", false},
		{"uri", "mailto:a@b", true},
		{"uri", "/relative", false},
		{"uri", "a:b", true},
		{"json-pointer", "", true},
		{"json-pointer", "/a~0b", true},
		{"json-pointer", "/a~2b", false},
		{"json-pointer", "a/b", false},
		{"relative-json-pointer", "0", true},
		{"relative-json-pointer", "1/foo", true},
		{"relative-json-pointer", "01", false},
		{"relative-json-pointer", "-1/foo", false},
		{"uuid", "123e4567-e89b-12d3-a456-426614174000", true},
		{"uuid", "123E4567-E89B-12D3-A456-426614174000", true},
		{"uuid", "123e4567e89b12d3a456426614174000", false},
		{"hostname", "example.com", true},
		{"hostname", "-bad.com", false},
		{"hostname", "exa_mple.com", false},
		{"hostname", "example.com.", false},
		{"ipv4", "1.2.3.4", true},
		{"ipv4", "1.2.3", false},
		{"ipv4", "::1", false},
		{"ipv6", "::1", true},
		{"ipv6", "1.2.3.4", false},
		{"duration", "P1Y2M3DT4H5M6S", true},
		{"duration", "1Y", false},
		{"duration", "P", false},
		{"duration", "P1W", true},
		{"email", "a@b.co", true},
		{"email", "a b@c.co", false},
		{"regex", "^a$", true},
		{"regex", "([", false},
		{"hostname", "example.com.", false},
		{"hostname", "", false},
		{"hostname", "a-b.c", true},
		{"idn-hostname", "例子.测试", true},
		{"idn-hostname", "例子.测试.", false},
		{"idn-hostname", "-a.b", false},
		{"idn-email", "a@例子.测试", true},
		{"idn-email", "a@b", true},
		{"date-time", "2023-01-01T12:00:00Z", true},
		{"date-time", "2023-01-01t12:00:00z", true},
		{"date-time", "2023-01-01 12:00:00Z", false},
		{"date-time", "2023-01-01T12:00:00", false},
		{"uri-reference", "/a%20b", true},
		{"uri-reference", "a b", false},
		{"uri-reference", "a%2", false},
		{"uri-template", "/a/{b}", true},
		{"uri-template", "/a/{b", false},
		{"uri-template", "a b", false},
		{"json-pointer-uri-fragment", "#/a", true},
		{"json-pointer-uri-fragment", "#a", false},
		{"json-pointer-uri-fragment", "/a", false},
		{"iri", "https://例子.测试/", true},
		{"iri", "/relative", false},
		{"time", "23:59:60-00:01", false},
		{"time", "00:00:60+00:01", true},
		{"time", "23:58:60Z", false},
		{"url", "file:/x", true},
		{"url", "ftp://x", true},
		{"url", "https://x/ a", true},
		{"regex", "(?<name>a)", true},
	}
	for _, testCase := range cases {
		if got := piFormatValid(testCase.format, testCase.value); got != testCase.want {
			t.Errorf("piFormatValid(%q, %q) = %v, want %v", testCase.format, testCase.value, got, testCase.want)
		}
	}
}

// The reference compiles a `regex` format with the ECMAScript `u` flag, which Go
// cannot match: RE2 has no lookaround or backreferences (pier rejects patterns
// the reference accepts), and it accepts a lone "{" as a literal where ECMAScript
// rejects a malformed quantifier (pier accepts a pattern the reference rejects).
// The difference is a documented RE2 limitation (AGENTS.md), pinned here so a
// later RE2 change shows up as a test failure rather than silently.
func TestPiFormatValidDocumentsTheRE2RegexGaps(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  bool // pier's answer; the reference answers the opposite
	}{
		{"a{2", true},    // reference: false (malformed quantifier under the u flag)
		{"(?=a)", false}, // reference: true (lookahead)
		{"a\\1", false},  // reference: true (backreference)
	} {
		if got := piFormatValid("regex", testCase.value); got != testCase.want {
			t.Fatalf("piFormatValid(\"regex\", %q) = %v, want %v", testCase.value, got, testCase.want)
		}
	}
}
