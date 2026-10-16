package interactive

import "strings"

// Syntax highlighting is not built in (D227). Upstream highlights code blocks
// with highlight.js; this port renders every language as a single themed
// code-block color, which is upstream's own no-valid-language fallback. The
// lang argument is kept because the markdown theme hook passes it (upstream
// hands it to highlight.js) and language detection still drives the tool
// renderers' presentation.

// HighlightCode renders a code block for the transcript.
func HighlightCode(code string, lang string) []string {
	return highlightCode(code, lang, ActiveTheme())
}

// highlightCode is HighlightCode with an explicit theme, for callers that
// captured one (the tool renderers' off-loop worker snapshots).
func highlightCode(code string, lang string, theme *Theme) []string {
	lines := strings.Split(code, "\n")
	for index, line := range lines {
		lines[index] = theme.Fg("mdCodeBlock", line)
	}
	return lines
}
