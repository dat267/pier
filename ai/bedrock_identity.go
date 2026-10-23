package ai

import "strings"

// bedrockIdentityUserAgent renders the identity contribution of upstream's
// bedrockUserAgent plus @aws-sdk/core escapeUserAgent. Detailed mode is a
// product/version pair, short mode a product pair, and custom mode a name.
func bedrockIdentityUserAgent(identity *ClientIdentity) string {
	name, version := "pi", AIVersion
	if identity != nil {
		if identity.Version != "" || identity.VersionSet {
			version = identity.Version
		}
		switch {
		case identity.CustomUserAgent != nil:
			name, version = *identity.CustomUserAgent, ""
		case identity.UserAgent == "short":
			version = ""
		case identity.UserAgent != "" && identity.UserAgent != "detailed":
			name, version = identity.UserAgent, ""
		}
	}
	name = escapeAWSUserAgentToken(name, false)
	version = escapeAWSUserAgentToken(version, true)
	prefix, rest, found := strings.Cut(name, "/")
	if !found {
		prefix, rest = "", name
	}
	if prefix == "api" {
		rest = strings.ToLower(rest)
	}
	parts := make([]string, 0, 3)
	for _, part := range []string{prefix, rest, version} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + "/" + parts[1]
	default:
		return parts[0] + "/" + parts[1] + "#" + parts[2]
	}
}

// SDK regexes use ASCII \w and replace each UTF-16 unit, not each rune.
// Names retain slash separators; versions allow # but not /.
func escapeAWSUserAgentToken(value string, version bool) string {
	var out strings.Builder
	for _, char := range value {
		allowed := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || strings.ContainsRune("!$%&'*+-.^_`|~", char) ||
			version && char == '#' || !version && char == '/'
		if allowed {
			out.WriteRune(char)
		} else {
			out.WriteByte('-')
			if char > 0xffff {
				out.WriteByte('-')
			}
		}
	}
	return out.String()
}
