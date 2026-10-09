package coding

import "strings"

// ToolNameMatchesPattern reports whether a selection entry matches a tool name. An entry is
// either an exact tool name or a pattern in which `*` matches any run of characters, the way
// upstream's `--tools` and `--exclude-tools` entries work (for example `mcp__radius__*`).
func ToolNameMatchesPattern(entry string, name string) bool {
	if entry == name {
		return true
	}
	if !strings.Contains(entry, "*") {
		return false
	}
	// Greedy walk with backtracking on the segments between the stars.
	segments := strings.Split(entry, "*")
	if !strings.HasPrefix(name, segments[0]) {
		return false
	}
	position := len(segments[0])
	for _, segment := range segments[1 : len(segments)-1] {
		index := strings.Index(name[position:], segment)
		if index < 0 {
			return false
		}
		position += index + len(segment)
	}
	last := segments[len(segments)-1]
	if last == "" {
		return true
	}
	return len(name)-position >= len(last) && strings.HasSuffix(name, last)
}

// matсhesAnyToolPattern reports whether a name matches any selection entry.
func matchesAnyToolPattern(entries []string, name string) bool {
	for _, entry := range entries {
		if ToolNameMatchesPattern(entry, name) {
			return true
		}
	}
	return false
}
