package coding

import (
	"fmt"
	"net/url"
	"strings"
)

// WebURL reports the http(s) URL value names, normalized for display and for a
// terminal hyperlink (pi v1.5.2 utils/web-url.ts webUrl; the login dialog had
// the same helper locally). The empty path becomes "/", so the displayed and
// linked forms match.
//
// This is a display normalizer, not a WHATWG URL parser: a value with no host
// is rejected (`http:example.com` links in the reference), and the host keeps
// its case and its Unicode spelling where the reference lowercases it and
// converts it to punycode. Every accepted value is a real http(s) URL, so a
// link still reaches the same site; only its bytes can differ from the
// reference's `href`.
//
// The WHATWG parser's input preprocessing is ported, because Go rejects any
// ASCII control character: leading and trailing C0 controls and spaces are
// trimmed, tabs and newlines are removed, and the remaining controls are
// percent-encoded, so `https://x.y/\n` still names the site.
func WebURL(value string) (string, bool) {
	parsed, err := url.Parse(whatwgPreprocess(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", false
	}
	normalized := *parsed
	normalized.Opaque = ""
	if normalized.Path == "" {
		normalized.Path = "/"
	}
	return normalized.String(), true
}

// whatwgPreprocess applies the URL parser's input cleanup: trim C0 controls
// and spaces from both ends, drop ASCII tab and newline anywhere, and
// percent-encode the other C0 controls and DEL, which every URL component's
// encode set holds.
func whatwgPreprocess(value string) string {
	value = strings.TrimFunc(value, func(r rune) bool { return r <= 0x20 })
	var builder strings.Builder
	for index := range len(value) {
		switch c := value[index]; {
		case c == '\t' || c == '\n' || c == '\r':
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&builder, "%%%02X", c)
		default:
			builder.WriteByte(c)
		}
	}
	return builder.String()
}

// DisplayURL renders a URL for display: normalized when it is a web URL,
// otherwise with each run of control characters replaced by one space, so an
// untrusted string cannot end a hyperlink early or split a line
// (pi v1.5.2 utils/web-url.ts displayUrl).
func DisplayURL(value string) string {
	if normalized, ok := WebURL(value); ok {
		return normalized
	}
	return replaceControlCharacterRuns(value)
}

// ReplaceControlCharacterRuns replaces each run of C0 and C1 control characters
// with one space (pi v1.5.2 packages/tui/src/utils.ts replaceControlCharacters),
// so an untrusted string printed as is cannot end a hyperlink early or split the
// output into extra lines.
func ReplaceControlCharacterRuns(value string) string { return replaceControlCharacterRuns(value) }

// replaceControlCharacterRuns replaces each run of C0 and C1 control
// characters with one space.
func replaceControlCharacterRuns(value string) string {
	var builder strings.Builder
	inRun := false
	for _, r := range value {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			if !inRun {
				builder.WriteByte(' ')
				inRun = true
			}
			continue
		}
		inRun = false
		builder.WriteRune(r)
	}
	return builder.String()
}
