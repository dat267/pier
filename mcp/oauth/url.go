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
	parsed, err := url.Parse(stripWHATWGURLWhitespace(value))
	if err != nil {
		return nil, err
	}
	// `new URL` removes dot segments while it parses the path; every consumer
	// (well-known candidates, resource matching) reads that normalized pathname.
	if escaped := parsed.EscapedPath(); escaped != "" {
		if normalized := normalizeDotSegments(escaped); normalized != escaped {
			setEscapedPath(parsed, normalized)
		}
	}
	return parsed, nil
}

// normalizeDotSegments applies the WHATWG path normalization `new URL`
// performs. A segment is a single dot when it is "." or "%2e", and a double dot
// for "..", ".%2e", "%2e." or "%2e%2e" (case-insensitive); both are removed,
// the double dot also dropping the preceding segment. Other segments keep their
// escapes exactly, and empty segments (a leading "//") are preserved.
func normalizeDotSegments(escapedPath string) string {
	segments := strings.Split(escapedPath, "/")
	changed := false
	for _, segment := range segments {
		if dotSegmentKind(segment) != 0 {
			changed = true
			break
		}
	}
	if !changed {
		return escapedPath
	}
	normalized := make([]string, 0, len(segments))
	for index, segment := range segments {
		switch dotSegmentKind(segment) {
		case 1:
			// A trailing single dot leaves the directory path with its slash.
			if index == len(segments)-1 {
				normalized = append(normalized, "")
			}
		case 2:
			// The leading empty segment of an absolute path is the root and
			// cannot be popped; any other segment, empty or not, can.
			if len(normalized) > 0 && !(len(normalized) == 1 && normalized[0] == "") {
				normalized = normalized[:len(normalized)-1]
			}
			if index == len(segments)-1 {
				normalized = append(normalized, "")
			}
		default:
			normalized = append(normalized, segment)
		}
	}
	return strings.Join(normalized, "/")
}

// dotSegmentKind classifies a raw path segment: 1 for a single dot, 2 for a
// double dot, 0 otherwise. Percent-decoding the whole segment catches the
// encoded spellings without treating escapes such as "..%2f" as a dot.
func dotSegmentKind(segment string) int {
	decoded, err := url.PathUnescape(segment)
	if err != nil {
		return 0
	}
	switch decoded {
	case ".":
		return 1
	case "..":
		return 2
	}
	return 0
}
