package markdown

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Definition is a link reference definition the document keeps: an entry of
// marked's tokens.links (pi v1.5.0 f9a26365, marked 18.0.11 Lexer.blockTokens
// and Tokenizer.def). Offset is the start of the top-level block holding it, so
// a caller re-lexing from an offset can pass the definitions before it.
type Definition struct {
	Offset int
	// Label is the normalized tag: lowercased, whitespace runs collapsed.
	Label string
	Href  string
	Title string
}

// linkMap is tokens.links: a normalized label's definition.
type linkMap map[string]Definition

// collectDefinitions walks blocks in document order the way blockTokens
// registers definitions: the first definition of a tag wins, and a later one
// is dropped from the tree (blockTokens does not push it). It runs after the
// containers' merges, so a definition joined into a paragraph is text. defs
// holds the definitions before blocks and is appended to.
func collectDefinitions(blocks []*Block, defs []Definition) ([]*Block, []Definition) {
	links := make(linkMap, len(defs))
	for _, def := range defs {
		if _, ok := links[def.Label]; !ok {
			links[def.Label] = def
		}
	}
	return keepDefinitions(blocks, -1, links, defs)
}

// keepDefinitions filters one token list. offset is the top-level block's
// start, or -1 when blocks are the top level themselves.
func keepDefinitions(blocks []*Block, offset int, links linkMap, defs []Definition) ([]*Block, []Definition) {
	kept := blocks[:0]
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		at := offset
		if at < 0 {
			at = b.Start
		}
		var ok bool
		if defs, ok = visitDefinitions(b, at, links, defs); ok {
			kept = append(kept, b)
			continue
		}
		// A dropped definition's newline is a one-character space token, which
		// blockTokens folds into the token before it; with none before, it is
		// pushed and renders as a blank line.
		if len(kept) == 0 && strings.HasSuffix(b.Raw, "\n") {
			if i+1 < len(blocks) && blocks[i+1].Kind == BlockSpace {
				blocks[i+1].Start = b.End - 1
			} else {
				kept = append(kept, &Block{Kind: BlockSpace, Start: b.End - 1, End: b.End})
			}
		}
	}
	return kept, defs
}

// visitDefinitions registers b if it is a definition, or the definitions
// nested in it, at the top-level block offset. ok=false drops b.
func visitDefinitions(b *Block, offset int, links linkMap, defs []Definition) ([]Definition, bool) {
	if b.Kind == BlockLinkDef {
		return registerDefinition(b, offset, links, defs)
	}
	if len(b.Blocks) > 0 {
		b.Blocks, defs = keepDefinitions(b.Blocks, offset, links, defs)
	}
	return defs, true
}

func registerDefinition(b *Block, offset int, links linkMap, defs []Definition) ([]Definition, bool) {
	label := normalizeLinkLabel(b.Text)
	if _, ok := links[label]; ok {
		return defs, false
	}
	title := b.Title
	if len(title) >= 2 {
		title = title[1 : len(title)-1]
	}
	def := Definition{Offset: offset, Label: label, Href: unescapePunctuation(b.Href), Title: unescapePunctuation(title)}
	links[label] = def
	return append(defs, def), true
}

// normalizeLinkLabel is Tokenizer.def's tag and Tokenizer.reflink's lookup key:
// `toLowerCase().replace(/\s+/g, ' ')`.
func normalizeLinkLabel(label string) string {
	var b strings.Builder
	space := false
	for _, r := range label {
		if isWhitespaceRune(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(unicode.ToLower(r))
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// unescapePunctuation is `replace(anyPunctuation, '$1')`: a backslash before
// Unicode punctuation or a symbol is dropped.
func unescapePunctuation(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			r, size := utf8.DecodeRuneInString(s[i+1:])
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				b.WriteString(s[i+1 : i+1+size])
				i += size
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// linksFor builds the lookup map of defs (each label appears once).
func linksFor(defs []Definition) linkMap {
	if len(defs) == 0 {
		return nil
	}
	links := make(linkMap, len(defs))
	for _, def := range defs {
		if _, ok := links[def.Label]; !ok {
			links[def.Label] = def
		}
	}
	return links
}

// mayHoldDefinition reports whether src can contain a definition (`[x]:`), so
// documents without one skip the tree walk.
func mayHoldDefinition(src string) bool {
	return strings.Contains(src, "]:")
}

// matchRefLink matches marked's reflink `^!?\[(label)\]\[(ref)\]`, then its
// nolink `^!?\[(ref)\](?:\[\])?`, at open (the `[`; start is open or the `!`
// before it). It returns the label text, the lookup string and the match end.
func matchRefLink(src string, start, open int) (label, ref string, end int, ok bool) {
	if labelEnd, refStart, ok := matchInlineLabel(src, open+1); ok {
		if refEnd, ok := matchBlockLabel(src, refStart); ok {
			return src[open+1 : labelEnd], src[refStart:refEnd], refEnd + 1, true
		}
	}
	refEnd, ok := matchBlockLabel(src, open+1)
	if !ok {
		return "", "", 0, false
	}
	end = refEnd + 1
	if strings.HasPrefix(src[end:], "[]") {
		end += 2
	}
	return src[open+1 : refEnd], src[open+1 : refEnd], end, true
}

// matchInlineLabel matches reflink's lazy _inlineLabel
//
//	(?:\[(?:\\[\s\S]|[^\[\]\\])*\]|\\[\s\S]|`+(?!`)[^`]*?`+(?!`)|``+(?=\])|[^\[\]\\`])*?
//
// from at up to the first `][` it can stop at. It returns the offset of that
// `]` and the offset after the `[`.
func matchInlineLabel(src string, at int) (labelEnd, refStart int, ok bool) {
	for at < len(src) {
		if src[at] == ']' {
			if at+1 < len(src) && src[at+1] == '[' {
				return at, at + 2, true
			}
			return 0, 0, false
		}
		switch src[at] {
		case '[':
			j := at + 1
			for j < len(src) && src[j] != ']' {
				if src[j] == '[' {
					return 0, 0, false
				}
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(src) {
				return 0, 0, false
			}
			at = j + 1
		case '\\':
			if at+1 >= len(src) {
				return 0, 0, false
			}
			_, size := utf8.DecodeRuneInString(src[at+1:])
			at += 1 + size
		case '`':
			run := at
			for run < len(src) && src[run] == '`' {
				run++
			}
			if close := strings.IndexByte(src[run:], '`'); close >= 0 {
				closeEnd := run + close
				for closeEnd < len(src) && src[closeEnd] == '`' {
					closeEnd++
				}
				at = closeEnd
			} else if run-at >= 2 && run < len(src) && src[run] == ']' {
				at = run
			} else {
				return 0, 0, false
			}
		default:
			at++
		}
	}
	return 0, 0, false
}

// matchBlockLabel matches `(?!\s*\])(?:\\[\s\S]|[^\[\]\\])+` and the `]` after
// it, returning that `]`'s offset.
func matchBlockLabel(src string, at int) (int, bool) {
	start := at
	blank := true
	for at < len(src) {
		switch c := src[at]; c {
		case '[':
			return 0, false
		case ']':
			if at == start || blank {
				return 0, false
			}
			return at, true
		case '\\':
			if at+1 >= len(src) {
				return 0, false
			}
			blank = false
			_, size := utf8.DecodeRuneInString(src[at+1:])
			at += 1 + size
			continue
		default:
			r, size := utf8.DecodeRuneInString(src[at:])
			if !isWhitespaceRune(r) {
				blank = false
			}
			at += size
			continue
		}
	}
	return 0, false
}

// parseRefLink is Tokenizer.reflink with outputLink (marked 18.0.11): a
// reference whose label is defined becomes a link or an image. ok=false leaves
// the `[` (or `!`) to the caller as text, which is also what marked emits for
// an undefined label.
func parseRefLink(src string, i int, image bool, state *inlineState) (Inline, int, bool) {
	open := i
	if image {
		open = i + 1
	}
	label, ref, end, ok := matchRefLink(src, i, open)
	if !ok {
		return Inline{}, 0, false
	}
	def, ok := state.links[normalizeLinkLabel(ref)]
	if !ok {
		return Inline{}, 0, false
	}
	text := strings.ReplaceAll(strings.ReplaceAll(label, `\[`, "["), `\]`, "]")
	previous := state.linkEmitted
	state.linkEmitted = false
	state.inLink = true
	children := parseInline(text, state)
	nested := state.linkEmitted
	state.linkEmitted = previous
	state.inLink = false
	kind := InlineImage
	if !image {
		// A link may not contain another link; the inner one is kept.
		if nested {
			return Inline{}, 0, false
		}
		state.linkEmitted = true
		kind = InlineLink
	}
	return Inline{Kind: kind, Text: text, Children: children, Dest: def.Href, Title: def.Title}, end, true
}

// maskRefLinks is inlineTokens' reflink mask: each reflinkSearch match
// (`reflink|nolink(?!\()`) whose raw last label is a defined tag becomes
// `[aaa]` of the same length, so emphasis cannot pair across it. A candidate
// whose text holds a link never becomes a link, so only the links inside its
// text are masked (maskReflink, marked 18.0.11).
func maskRefLinks(src string, links linkMap) string {
	var masked []byte
	eachRefLinkMatch(src, func(start, end int) {
		match := src[start:end]
		refStart := strings.LastIndexByte(match, '[')
		if _, defined := links[match[refStart+1:len(match)-1]]; !defined {
			return
		}
		if masked == nil {
			masked = []byte(src)
		}
		if refStart > 1 && match[0] != '!' {
			if text := match[1 : refStart-1]; linkInText(text, links) {
				copy(masked[start+1:], maskRefLinks(text, links))
				masked[start+refStart-1] = ']'
				masked[start+refStart] = '['
				for j := start + refStart + 1; j < end-1; j++ {
					masked[j] = 'a'
				}
				masked[end-1] = ']'
				return
			}
		}
		masked[start] = '['
		for j := start + 1; j < end-1; j++ {
			masked[j] = 'a'
		}
		masked[end-1] = ']'
	})
	if masked == nil {
		return src
	}
	return string(masked)
}

// eachRefLinkMatch calls match for each reflinkSearch match in src, left to
// right as a global regex scan finds them.
func eachRefLinkMatch(src string, match func(start, end int)) {
	for i := 0; i < len(src); i++ {
		start := i
		open := i
		if src[i] == '!' {
			if i+1 >= len(src) || src[i+1] != '[' {
				continue
			}
			open = i + 1
		} else if src[i] != '[' {
			continue
		}
		end, ok := matchRefLinkSearch(src, open)
		if !ok && open != start {
			// `!?` backtracks: retry at the `[`.
			start = open
			end, ok = matchRefLinkSearch(src, open)
		}
		if !ok {
			continue
		}
		match(start, end)
		i = end - 1
	}
}

// linkInText is Lexer.linkInText (marked 18.0.11): text holds an inline link
// (a blockSkip link span the link rule accepts, not an image), or a defined
// reference that is not an image and whose own text holds no link.
func linkInText(text string, links linkMap) bool {
	if !strings.Contains(text, "[") {
		return false
	}
	for i := 0; i < len(text); i++ {
		end := -1
		switch text[i] {
		case '`':
			if i == 0 || text[i-1] != '`' {
				end = emphasisCodeMaskEnd(text, i)
			}
		case '<':
			if i+1 < len(text) && text[i+1] != ' ' {
				if offset := strings.IndexAny(text[i+1:], "<>"); offset >= 0 && text[i+1+offset] == '>' {
					end = i + offset + 2
				}
			}
		case '[':
			if end = emphasisLinkMaskEnd(text, i); end > i && (i == 0 || text[i-1] != '!') {
				if _, _, ok := parseLink(text[i:end], 0, false, &inlineState{}); ok {
					return true
				}
			}
		}
		if end > i {
			i = end - 1
		}
	}
	found := false
	eachRefLinkMatch(text, func(start, end int) {
		match := text[start:end]
		refStart := strings.LastIndexByte(match, '[')
		if found || match[0] == '!' {
			return
		}
		if _, defined := links[match[refStart+1:len(match)-1]]; !defined {
			return
		}
		if refStart > 1 && linkInText(match[1:refStart-1], links) {
			return
		}
		found = true
	})
	return found
}

// matchRefLinkSearch is one reflinkSearch alternative match at open: reflink,
// or nolink not followed by `(`.
func matchRefLinkSearch(src string, open int) (int, bool) {
	if labelEnd, refStart, ok := matchInlineLabel(src, open+1); ok && labelEnd >= 0 {
		if refEnd, ok := matchBlockLabel(src, refStart); ok {
			return refEnd + 1, true
		}
	}
	refEnd, ok := matchBlockLabel(src, open+1)
	if !ok {
		return 0, false
	}
	end := refEnd + 1
	if strings.HasPrefix(src[end:], "[]") {
		end += 2
	}
	if end < len(src) && src[end] == '(' {
		// `(?!\()` after the optional `[]` backtracks to the bare `[ref]`.
		if end != refEnd+1 && src[refEnd+1] != '(' {
			return refEnd + 1, true
		}
		return 0, false
	}
	return end, true
}
