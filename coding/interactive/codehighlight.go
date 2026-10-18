package interactive

import (
	"strings"
	"sync"

	"github.com/dat267/pier/internal/highlight"
)

// This file is the port of the reference's
// packages/coding-agent/src/utils/syntax-highlight.ts and the terminal half of
// theme/theme.ts's buildCliHighlightTheme. It supersedes D227: syntax
// highlighting is built in again, and a code block with a registered language
// gets per-token colors instead of one code-block color.
//
// The reference builds highlight.js's HTML and then walks it, matching each
// `<span class="hljs-...">` to a scope. The port's `highlight` package already
// returns the same scope tree, so this walks the tree and skips the HTML
// round-trip; the two are equivalent because the HTML walk was exactly
// escape-then-unescape.

// highlightFormatter is one entry of the reference's CliHighlightTheme: a
// foreground token or a text style, not a closure, so a lookup allocates
// nothing.
type highlightFormatter struct {
	color string
	style string // "", "italic", "bold", "underline"
}

func (f highlightFormatter) apply(theme *Theme, text string) string {
	switch f.style {
	case "italic":
		return theme.Italic(text)
	case "bold":
		return theme.Bold(text)
	case "underline":
		return theme.Underline(text)
	}
	return theme.Fg(f.color, text)
}

// exactHighlightFormatter is the reference's buildCliHighlightTheme map, as a
// switch. Scope names are highlight.js class names without the `hljs-` prefix.
func exactHighlightFormatter(scope string) (highlightFormatter, bool) {
	switch scope {
	case "keyword":
		return highlightFormatter{color: "syntaxKeyword"}, true
	case "built_in":
		return highlightFormatter{color: "syntaxType"}, true
	case "literal", "number":
		return highlightFormatter{color: "syntaxNumber"}, true
	case "regexp", "string":
		return highlightFormatter{color: "syntaxString"}, true
	case "subst":
		return highlightFormatter{color: "text"}, true
	case "comment", "doctag":
		return highlightFormatter{color: "syntaxComment"}, true
	case "meta":
		return highlightFormatter{color: "muted"}, true
	case "function", "title":
		return highlightFormatter{color: "syntaxFunction"}, true
	case "class", "type":
		return highlightFormatter{color: "syntaxType"}, true
	case "tag":
		return highlightFormatter{color: "syntaxPunctuation"}, true
	case "name":
		return highlightFormatter{color: "syntaxKeyword"}, true
	case "attr", "variable", "params":
		return highlightFormatter{color: "syntaxVariable"}, true
	case "operator":
		return highlightFormatter{color: "syntaxOperator"}, true
	case "punctuation":
		return highlightFormatter{color: "syntaxPunctuation"}, true
	case "emphasis":
		return highlightFormatter{style: "italic"}, true
	case "strong":
		return highlightFormatter{style: "bold"}, true
	case "link":
		return highlightFormatter{style: "underline"}, true
	case "addition":
		return highlightFormatter{color: "toolDiffAdded"}, true
	case "deletion":
		return highlightFormatter{color: "toolDiffRemoved"}, true
	}
	return highlightFormatter{}, false
}

// scopeHighlightFormatter is the reference's getScopeFormatter: exact scope,
// then the prefix before `.`, then the prefix before `-`.
func scopeHighlightFormatter(scope string) (highlightFormatter, bool) {
	if f, ok := exactHighlightFormatter(scope); ok {
		return f, true
	}
	if i := strings.IndexByte(scope, '.'); i != -1 {
		if f, ok := exactHighlightFormatter(scope[:i]); ok {
			return f, true
		}
	}
	if i := strings.IndexByte(scope, '-'); i != -1 {
		if f, ok := exactHighlightFormatter(scope[:i]); ok {
			return f, true
		}
	}
	return highlightFormatter{}, false
}

// activeHighlightFormatter is the reference's getActiveFormatter: the
// innermost scope with a formatter wins. The CLI theme sets no default, so an
// unstyled fragment stays unstyled.
func activeHighlightFormatter(scopes []string) (highlightFormatter, bool) {
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i] == "" {
			continue
		}
		if f, ok := scopeHighlightFormatter(scopes[i]); ok {
			return f, true
		}
	}
	return highlightFormatter{}, false
}

// writeHighlightedText colors one text leaf with the active scope. Text is
// colored line by line and an empty line stays empty (upstream #10143).
func writeHighlightedText(out *strings.Builder, value string, theme *Theme, scopes []string) {
	if value == "" {
		return
	}
	f, ok := activeHighlightFormatter(scopes)
	if !ok {
		out.WriteString(value)
		return
	}
	start := 0
	for {
		i := strings.IndexByte(value[start:], '\n')
		var line string
		if i == -1 {
			line = value[start:]
		} else {
			line = value[start : start+i]
		}
		if line != "" {
			out.WriteString(f.apply(theme, line))
		}
		if i == -1 {
			return
		}
		out.WriteByte('\n')
		start += i + 1
	}
}

// renderHighlightedNodes colors the port's scope tree. A node with a class is a
// scope that nests; a node without one is transparent. A sublanguage node's
// class is the language name, which has no `hljs-` prefix, so it contributes no
// scope (the reference's getScopeFromSpanTag skips it).
func renderHighlightedNodes(nodes []*highlight.Node, theme *Theme) string {
	var out strings.Builder
	renderHighlightedNode(&out, nodes, theme, nil)
	return out.String()
}

func renderHighlightedNode(out *strings.Builder, nodes []*highlight.Node, theme *Theme, scopes []string) {
	for _, n := range nodes {
		if n.Kind == "" {
			if n.Children == nil {
				writeHighlightedText(out, n.Text, theme, scopes)
				continue
			}
			renderHighlightedNode(out, n.Children, theme, scopes)
			continue
		}
		scope := ""
		if !n.Sublanguage {
			scope = n.Kind
		}
		renderHighlightedNode(out, n.Children, theme, append(scopes, scope))
	}
}

func plainCodeBlock(code string, theme *Theme) []string {
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		lines[i] = theme.Fg("mdCodeBlock", line)
	}
	return lines
}

// highlightCode is the reference's standalone highlightCode: a registered
// language gets token colors, anything else gets the plain code-block color,
// and an internal failure returns the raw lines.
func highlightCode(code string, lang string, theme *Theme) []string {
	return highlightCodeCore(code, lang, theme, true)
}

// HighlightCode renders a code block for the transcript's markdown theme. It
// is highlightCode with the reference's markdown fallback: a failure leaves the
// fence in the plain code-block color rather than dropping its styling.
func HighlightCode(code string, lang string) []string {
	return highlightCodeCore(code, lang, ActiveTheme(), false)
}

func highlightCodeCore(code string, lang string, theme *Theme, rawFallback bool) []string {
	if !highlight.SupportsLanguage(lang) {
		return plainCodeBlock(code, theme)
	}
	if lines, ok := lookupHighlightCache(code, lang); ok {
		return lines
	}
	result, err := highlight.HighlightWith(code, lang, true)
	if err != nil {
		if rawFallback {
			return strings.Split(code, "\n")
		}
		return plainCodeBlock(code, theme)
	}
	lines := strings.Split(renderHighlightedNodes(result.Nodes, theme), "\n")
	storeHighlightCache(code, lang, lines)
	return lines
}

// highlightCache memoizes rendered code-block lines by language and source. A
// streaming turn re-renders the whole assistant message per delta, and the
// markdown renderer re-highlights a fence inside the re-lex window even when
// the fence text is unchanged; the Oct 18 streak-start dump caught the UI loop
// inside highlight.HighlightWith (regex backtracking) at a 100ms stall. The
// cache is thread-safe because a D239 worker renders a snapshot off-loop, and
// setGlobalTheme clears it so a theme change cannot serve stale colors. The
// returned lines are shared read-only (the markdown renderer only reads them).
var highlightCache = struct {
	mu      sync.Mutex
	entries map[string][]string
}{}

// highlightCacheMaxEntries bounds the cache; overflow clears it wholesale
// rather than tracking recency, since a hit is a pure win and recomputing is
// exactly what happened before the cache existed.
const highlightCacheMaxEntries = 128

func lookupHighlightCache(code string, lang string) ([]string, bool) {
	highlightCache.mu.Lock()
	defer highlightCache.mu.Unlock()
	lines, ok := highlightCache.entries[lang+"\x00"+code]
	return lines, ok
}

func storeHighlightCache(code string, lang string, lines []string) {
	highlightCache.mu.Lock()
	defer highlightCache.mu.Unlock()
	if len(highlightCache.entries) >= highlightCacheMaxEntries {
		highlightCache.entries = nil
	}
	if highlightCache.entries == nil {
		highlightCache.entries = map[string][]string{}
	}
	highlightCache.entries[lang+"\x00"+code] = lines
}

// ClearHighlightCache drops every memoized code block; setGlobalTheme calls it
// on a theme change, and tests call it for isolation.
func ClearHighlightCache() {
	highlightCache.mu.Lock()
	highlightCache.entries = nil
	highlightCache.mu.Unlock()
}
