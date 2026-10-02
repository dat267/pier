package durable

import (
	"context"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Port of tools/path-utils.ts: tool input paths may carry Unicode spaces, an
// `@` prefix, or filename characters a terminal rewrote.

var unicodeSpacesPattern = regexp.MustCompile(`[\x{00A0}\x{2000}-\x{200A}\x{202F}\x{205F}\x{3000}]`)

// narrowNoBreakSpace is upstream NARROW_NO_BREAK_SPACE.
const narrowNoBreakSpace = "\u202F"

var meridiemPattern = regexp.MustCompile(`(?i) (AM|PM)\.`)

// NormalizeToolPath replaces Unicode spaces with ASCII spaces and strips one
// leading `@`.
func NormalizeToolPath(path string) string {
	normalized := unicodeSpacesPattern.ReplaceAllString(path, " ")
	return strings.TrimPrefix(normalized, "@")
}

// ResolveToolPath normalizes and resolves a tool path against the environment.
func ResolveToolPath(ctx context.Context, env FileSystem, path string) (string, error) {
	return env.AbsolutePath(NormalizeToolPath(path), ctx)
}

// ResolveReadToolPath resolves a read path, trying the filename variants a
// terminal may have produced: a no-break space before AM/PM, NFD
// normalization, and typographic apostrophes.
func ResolveReadToolPath(ctx context.Context, env FileSystem, path string) (string, error) {
	resolved, err := ResolveToolPath(ctx, env, path)
	if err != nil {
		return "", err
	}
	apostrophes := strings.ReplaceAll(resolved, "'", "\u2019")
	meridiem := meridiemPattern.ReplaceAllString(resolved, narrowNoBreakSpace+"$1.")
	variants := []string{
		resolved,
		meridiem,
		norm.NFD.String(resolved),
		apostrophes,
		strings.ReplaceAll(norm.NFD.String(resolved), "'", "\u2019"),
	}
	seen := map[string]struct{}{}
	for _, variant := range variants {
		if _, duplicate := seen[variant]; duplicate {
			continue
		}
		seen[variant] = struct{}{}
		exists, err := env.Exists(variant, ctx)
		if err != nil {
			return "", err
		}
		if exists {
			return variant, nil
		}
	}
	return resolved, nil
}
