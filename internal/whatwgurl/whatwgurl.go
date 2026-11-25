// Package whatwgurl holds the parts of the WHATWG URL parser that Go's
// net/url does not apply and that more than one layer needs. ai, coding and
// mcp share this one copy.
package whatwgurl

import (
	"net/url"
	"strings"
)

// StripWhitespace applies the parser's preprocessing, which URL.canParse and
// `new URL` run before parsing: leading and trailing C0 controls or spaces
// are stripped, and ASCII tab, CR and LF are removed anywhere. Go's url.Parse
// rejects those characters instead, so an otherwise valid URL with a stray
// space or newline would fail.
func StripWhitespace(value string) string {
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
