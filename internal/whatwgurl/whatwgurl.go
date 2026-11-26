// Package whatwgurl holds the parts of the WHATWG URL parser that Go's
// net/url does not apply and that more than one layer needs. ai, coding and
// mcp share this one copy.
package whatwgurl

import (
	"fmt"
	"net/url"
	"strings"
)

// StripWhitespace applies the parser's preprocessing, which URL.canParse and
// `new URL` run before parsing, plus the percent-encode step the encode sets
// apply to a parsed component: leading and trailing C0 controls or spaces are
// stripped, ASCII tab, CR and LF are removed anywhere, and every remaining C0
// control or DEL is percent-encoded. Go's url.Parse rejects all of those
// characters instead, so an otherwise valid URL with a stray control byte
// would fail — Node percent-encodes an INTERIOR one rather than removing it
// (`https://x.y/a\x01b` becomes `a%01b`).
func StripWhitespace(value string) string {
	value = strings.TrimFunc(value, func(r rune) bool { return r <= 0x20 })
	if !strings.ContainsFunc(value, needsURLEncoding) {
		return value
	}
	var builder strings.Builder
	builder.Grow(len(value) + 8)
	for index := 0; index < len(value); index++ {
		c := value[index]
		switch {
		case c == '\t' || c == '\r' || c == '\n':
		case needsURLEncoding(rune(c)):
			fmt.Fprintf(&builder, "%%%02X", c)
		default:
			builder.WriteByte(c)
		}
	}
	return builder.String()
}

// needsURLEncoding reports a byte the parser removes (tab, CR, LF) or
// percent-encodes (the other C0 controls and DEL).
func needsURLEncoding(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// Origin renders a URL's origin as URL.origin does: the scheme and host
// lowercased, and the scheme's default port dropped.
func Origin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	switch scheme {
	case "http":
		host = strings.TrimSuffix(host, ":80")
	case "https":
		host = strings.TrimSuffix(host, ":443")
	}
	return scheme + "://" + host
}

// ResolvePath applies the URL parser's path state: "." and ".." segments are
// resolved, repeated slashes are kept, and a ".." cannot climb above the root.
func ResolvePath(path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	out := make([]string, 0, len(segments))
	for index, segment := range segments {
		last := index == len(segments)-1
		switch segment {
		case ".":
			// A trailing "." keeps its slash.
			if last {
				out = append(out, "")
			}
			continue
		case "..":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
			continue
		}
		out = append(out, segment)
	}
	return strings.Join(out, "/")
}
