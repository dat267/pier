package oauth

import (
	"net/url"
	"strings"
)

// stripWHATWGURLWhitespace applies the WHATWG URL parser's preprocessing,
// which URL.canParse and `new URL` run before parsing: leading and trailing C0
// controls or spaces are stripped, and ASCII tab, CR and LF are removed
// anywhere. Go's url.Parse rejects those characters instead.
func stripWHATWGURLWhitespace(value string) string {
	value = strings.TrimFunc(value, func(r rune) bool { return r <= 0x20 })
	if !strings.ContainsAny(value, "\t\r\n") {
		return value
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\r', '\n':
			return -1
		}
		return r
	}, value)
}

// parseWHATWGURL parses an externally supplied URL (a config value, a server
// header or a discovery document member) the way the reference's `new URL`
// does before reading its components.
func parseWHATWGURL(value string) (*url.URL, error) {
	return url.Parse(stripWHATWGURLWhitespace(value))
}
