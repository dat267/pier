package tui

import (
	"strings"
)

// Port of the wrapping half of src/utils.ts: ANSI-aware word wrapping.

// isSpaceRune reports a plain space (the wrap tokenizer only splits on the
// space character).
func isSpaceRune(r rune) bool { return r == ' ' }

// splitIntoTokensWithAnsi splits into word/space tokens, keeping ANSI codes
// attached to the following visible content. CJK characters break anywhere.
func splitIntoTokensWithAnsi(text string) []string {
	// A line that is entirely printable ASCII (plain prose, no escapes) needs no
	// tokenizer state at all: every word/space run is a substring of text, so the
	// common case allocates only the result slice.
	if isPrintableASCII(text) {
		return splitAsciiRuns(text)
	}

	var tokens []string
	var current strings.Builder
	pendingAnsi := ""
	currentKind := "" // "space" | "word" | ""

	flushCurrent := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
			currentKind = ""
		}
	}

	index := 0
	for index < len(text) {
		if code, length := ExtractANSICode(text, index); code != "" {
			pendingAnsi += code
			index += length
			continue
		}

		// The visible run ends at the next ESC that begins a valid ANSI code.
		// Searching from index+1 and skipping escapes ExtractANSICode rejects
		// keeps a malformed ESC (a lone escape in the text) as visible content
		// and guarantees progress; upstream skips them the same way.
		end := len(text)
		if start := nextAnsiStart(text, index); start >= 0 {
			end = start
		}

		chunk := text[index:end]
		// Printable ASCII is the dominant case (plain prose). Every byte is its
		// own grapheme and can never be a CJK break, so split word/space runs by
		// slicing instead of running the grapheme segmenter, converting each
		// segment to runes and binary-searching the CJK ranges for every byte.
		// This is upstream's isPrintableAscii fast path.
		if isPrintableASCII(chunk) {
			for runStart := 0; runStart < len(chunk); {
				isSpace := chunk[runStart] == ' '
				runEnd := runStart + 1
				for runEnd < len(chunk) && (chunk[runEnd] == ' ') == isSpace {
					runEnd++
				}
				kind := "word"
				if isSpace {
					kind = "space"
				}
				if current.Len() > 0 && currentKind != kind {
					flushCurrent()
				}
				if pendingAnsi != "" {
					current.WriteString(pendingAnsi)
					pendingAnsi = ""
				}
				currentKind = kind
				current.WriteString(chunk[runStart:runEnd])
				runStart = runEnd
			}
			index = end
			continue
		}

		for _, segment := range segmentGraphemes(chunk) {
			runes := []rune(segment)
			segmentIsSpace := isSpaceRune(runes[0]) && len(runes) == 1
			if !segmentIsSpace && isCJKBreakRune(runes[0]) {
				flushCurrent()
				tokens = append(tokens, pendingAnsi+segment)
				pendingAnsi = ""
				continue
			}
			segmentKind := "word"
			if segmentIsSpace {
				segmentKind = "space"
			}
			if current.Len() > 0 && currentKind != segmentKind {
				flushCurrent()
			}
			if pendingAnsi != "" {
				current.WriteString(pendingAnsi)
				pendingAnsi = ""
			}
			currentKind = segmentKind
			current.WriteString(segment)
		}

		index = end
	}

	// Remaining pending ANSI codes attach to the last token.
	if pendingAnsi != "" {
		if current.Len() > 0 {
			current.WriteString(pendingAnsi)
		} else if len(tokens) > 0 {
			tokens[len(tokens)-1] += pendingAnsi
		} else {
			current.WriteString(pendingAnsi)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// nextAnsiStart returns the byte index of the next ESC in text at or after
// from+1 that begins a valid ANSI code, or -1. Searching past from and skipping
// escapes ExtractANSICode rejects keeps a malformed ESC as visible content and
// guarantees the caller advances; upstream's wrap helpers skip them the same
// way.
func nextAnsiStart(text string, from int) int {
	for search := from + 1; search <= len(text); {
		escape := strings.IndexByte(text[search:], '\x1b')
		if escape < 0 {
			return -1
		}
		candidate := search + escape
		if code, _ := ExtractANSICode(text, candidate); code != "" {
			return candidate
		}
		search = candidate + 1
	}
	return -1
}

// splitAsciiRuns splits printable ASCII into maximal word/space runs, each a
// substring of text.
func splitAsciiRuns(text string) []string {
	var tokens []string
	for start := 0; start < len(text); {
		isSpace := text[start] == ' '
		end := start + 1
		for end < len(text) && (text[end] == ' ') == isSpace {
			end++
		}
		tokens = append(tokens, text[start:end])
		start = end
	}
	return tokens
}

// breakLongWord breaks an over-long token, re-establishing styles per piece.
func breakLongWord(word string, maxWidth int, tracker *ansiCodeTracker) []string {
	var pieces []string
	current := tracker.activeCodes()
	currentWidth := 0
	index := 0
	for index < len(word) {
		if code, length := ExtractANSICode(word, index); code != "" {
			current += code
			tracker.process(code)
			index += length
			continue
		}
		end := len(word)
		if start := nextAnsiStart(word, index); start >= 0 {
			end = start
		}
		for _, segment := range segmentGraphemes(word[index:end]) {
			w := graphemeWidth(segment)
			if currentWidth+w > maxWidth {
				pieces = append(pieces, current)
				current = tracker.activeCodes()
				currentWidth = 0
			}
			current += segment
			currentWidth += w
		}
		index = end
	}
	if current != "" {
		pieces = append(pieces, current)
	}
	if len(pieces) == 0 {
		pieces = []string{""}
	}
	return pieces
}

// WrapTextWithAnsi wraps text to width with ANSI codes preserved: word
// wrapping only, no padding, no background colors. Active ANSI codes are
// carried across line breaks and newlines in the input.
func WrapTextWithAnsi(text string, width int) []string {
	if text == "" {
		return []string{""}
	}

	// Newlines: each line wraps separately with ANSI state carried over.
	inputLines := splitLines(text)
	var result []string
	tracker := &ansiCodeTracker{}

	for _, inputLine := range inputLines {
		prefix := ""
		if len(result) > 0 {
			prefix = tracker.activeCodes()
		}
		wrappedLines := wrapSingleLine(prefix+inputLine, width)
		result = append(result, wrappedLines...)
		updateTrackerFromText(inputLine, tracker)
	}

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// splitLines splits on \r\n, \r, and \n (upstream's /\r\n|\r|\n/).
func splitLines(text string) []string {
	var lines []string
	start := 0
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '\r':
			lines = append(lines, text[start:index])
			if index+1 < len(text) && text[index+1] == '\n' {
				index++
			}
			start = index + 1
		case '\n':
			lines = append(lines, text[start:index])
			start = index + 1
		}
	}
	return append(lines, text[start:])
}

func wrapSingleLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	visibleLength := VisibleWidth(line)
	if visibleLength <= width {
		return []string{line}
	}

	var wrapped []string
	tracker := &ansiCodeTracker{}
	tokens := splitIntoTokensWithAnsi(line)

	var currentLine strings.Builder
	currentVisibleLength := 0

	for _, token := range tokens {
		tokenVisibleLength := VisibleWidth(token)
		// Upstream tests `token.trim() === ""` on the raw token, so a token
		// carrying an ANSI code is not whitespace even when it ends in a space.
		isWhitespace := IsWhitespaceChar(token)

		// A token longer than the width breaks character by character.
		if tokenVisibleLength > width && !isWhitespace {
			if currentLine.Len() > 0 {
				if lineEndReset := tracker.lineEndReset(); lineEndReset != "" {
					currentLine.WriteString(lineEndReset)
				}
				wrapped = append(wrapped, currentLine.String())
				currentLine.Reset()
				currentVisibleLength = 0
			}
			broken := breakLongWord(token, width, tracker)
			for index := 0; index < len(broken)-1; index++ {
				wrapped = append(wrapped, broken[index])
			}
			currentLine.WriteString(broken[len(broken)-1])
			currentVisibleLength = VisibleWidth(currentLine.String())
			continue
		}

		// Adding the token would exceed the width: wrap.
		if currentVisibleLength+tokenVisibleLength > width && currentVisibleLength > 0 {
			lineToWrap := strings.TrimRight(currentLine.String(), " \t")
			if lineEndReset := tracker.lineEndReset(); lineEndReset != "" {
				lineToWrap += lineEndReset
			}
			wrapped = append(wrapped, lineToWrap)
			if isWhitespace {
				// Do not start a new line with whitespace.
				currentLine.Reset()
				currentLine.WriteString(tracker.activeCodes())
				currentVisibleLength = 0
			} else {
				currentLine.Reset()
				currentLine.WriteString(tracker.activeCodes() + token)
				currentVisibleLength = tokenVisibleLength
			}
		} else {
			currentLine.WriteString(token)
			currentVisibleLength += tokenVisibleLength
		}

		updateTrackerFromText(token, tracker)
	}

	if currentLine.Len() > 0 {
		// No reset on the final line; the caller handles it.
		wrapped = append(wrapped, currentLine.String())
	}

	// Trailing whitespace can push lines past the requested width.
	if len(wrapped) > 0 {
		trimmed := make([]string, 0, len(wrapped))
		for _, line := range wrapped {
			trimmed = append(trimmed, strings.TrimRight(line, " \t"))
		}
		return trimmed
	}
	return []string{""}
}
