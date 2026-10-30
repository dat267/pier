package tui

import "unicode/utf8"

// Port of src/undo-stack.ts, src/kill-ring.ts, and src/word-navigation.ts.

// UndoStack stores state snapshots. Upstream clones on push with
// structuredClone; Go callers pass already-detached values, and the stack
// stores them as-is (divergence D59: no deep clone).
type UndoStack[S any] struct {
	stack []S
}

// Push records a snapshot.
func (s *UndoStack[S]) Push(state S) { s.stack = append(s.stack, state) }

// Pop returns the most recent snapshot.
func (s *UndoStack[S]) Pop() (S, bool) {
	var zero S
	if len(s.stack) == 0 {
		return zero, false
	}
	last := len(s.stack) - 1
	state := s.stack[last]
	s.stack[last] = zero
	s.stack = s.stack[:last]
	return state, true
}

// Clear drops all snapshots.
func (s *UndoStack[S]) Clear() { s.stack = nil }

// Len returns the snapshot count.
func (s *UndoStack[S]) Len() int { return len(s.stack) }

// KillRing is a ring buffer for Emacs-style kill/yank operations.
type KillRing struct {
	ring []string
}

// KillRingPushOptions configure a push.
type KillRingPushOptions struct {
	// Prepend selects prepend (backward deletion) versus append (forward).
	Prepend bool
	// Accumulate merges with the most recent entry.
	Accumulate bool
}

// Push adds killed text to the ring.
func (k *KillRing) Push(text string, options KillRingPushOptions) {
	if text == "" {
		return
	}
	if options.Accumulate && len(k.ring) > 0 {
		last := k.ring[len(k.ring)-1]
		k.ring = k.ring[:len(k.ring)-1]
		if options.Prepend {
			k.ring = append(k.ring, text+last)
		} else {
			k.ring = append(k.ring, last+text)
		}
		return
	}
	k.ring = append(k.ring, text)
}

// Peek returns the most recent entry without modifying the ring.
func (k *KillRing) Peek() (string, bool) {
	if len(k.ring) == 0 {
		return "", false
	}
	return k.ring[len(k.ring)-1], true
}

// Rotate moves the last entry to the front (for yank-pop cycling).
func (k *KillRing) Rotate() {
	if len(k.ring) > 1 {
		last := k.ring[len(k.ring)-1]
		k.ring = append([]string{last}, k.ring[:len(k.ring)-1]...)
	}
}

// Len returns the ring size.
func (k *KillRing) Len() int { return len(k.ring) }

// punctuationChars is the ASCII punctuation set used for word boundaries
// (upstream PUNCTUATION_REGEX).
var punctuationChars = map[rune]bool{
	'(': true, ')': true, '{': true, '}': true, '[': true, ']': true, '<': true, '>': true,
	'.': true, ',': true, ';': true, ':': true, '\'': true, '"': true, '!': true, '?': true,
	'+': true, '-': true, '=': true, '*': true, '/': true, '\\': true, '|': true, '&': true,
	'%': true, '^': true, '$': true, '#': true, '@': true, '~': true, '`': true,
}

// WordSegment is a word-granularity segment.
type WordSegment struct {
	Segment    string
	Index      int
	IsWordLike bool
}

// WordSegments segments text with word granularity.
//
// Upstream uses Intl.Segmenter (full UAX #29 word segmentation); the Go port
// implements the practical rules observed from ICU (divergence D58): word
// characters are letters, digits, marks, and connector punctuation; a single
// mid-word "." merges between alphanumerics, "'" between letters, and ","
// between digits; whitespace runs and individual punctuation cells are
// non-word segments.
func WordSegments(text string) []WordSegment {
	// Walk the source directly so segments can reference its bytes instead of
	// allocating rune and byte-offset tables.
	var segments []WordSegment
	byteOffset := 0
	// Keep indices compatible with encoding each decoded rune to a Go string;
	// invalid UTF-8 therefore advances by the RuneError encoding width.
	logicalOffset := 0
	for byteOffset < len(text) {
		startByte := byteOffset
		startIndex := logicalOffset
		r, size := utf8.DecodeRuneInString(text[byteOffset:])

		if isWhitespaceRune(r) {
			byteOffset += size
			logicalOffset += utf8.RuneLen(r)
			for byteOffset < len(text) {
				next, nextSize := utf8.DecodeRuneInString(text[byteOffset:])
				if !isWhitespaceRune(next) {
					break
				}
				byteOffset += nextSize
				logicalOffset += utf8.RuneLen(next)
			}
			segments = append(segments, WordSegment{
				Segment: text[startByte:byteOffset], Index: startIndex,
			})
			continue
		}

		if !isWordChar(r) {
			segment := text[byteOffset : byteOffset+size]
			if r == utf8.RuneError && size == 1 {
				segment = string(r)
			}
			segments = append(segments, WordSegment{Segment: segment, Index: startIndex})
			byteOffset += size
			logicalOffset += utf8.RuneLen(r)
			continue
		}

		previous := r
		byteOffset += size
		logicalOffset += utf8.RuneLen(r)
		for byteOffset < len(text) {
			current, currentSize := utf8.DecodeRuneInString(text[byteOffset:])
			if isWordChar(current) {
				byteOffset += currentSize
				logicalOffset += utf8.RuneLen(current)
				previous = current
				continue
			}

			afterOffset := byteOffset + currentSize
			if afterOffset < len(text) {
				after, afterSize := utf8.DecodeRuneInString(text[afterOffset:])
				if isWordChar(after) && isMidWordChar(current, previous, after) {
					byteOffset = afterOffset + afterSize
					logicalOffset += utf8.RuneLen(current) + utf8.RuneLen(after)
					previous = after
					continue
				}
			}
			break
		}
		segments = append(segments, WordSegment{
			Segment: text[startByte:byteOffset], Index: startIndex, IsWordLike: true,
		})
	}
	return segments
}

func isMidWordChar(mid rune, before rune, after rune) bool {
	switch mid {
	case '.':
		return isWordChar(before) && isWordChar(after)
	case '\'':
		return isLetterRune(before) && isLetterRune(after)
	case ',':
		return isDigitRune(before) && isDigitRune(after)
	}
	return false
}

func isWordChar(r rune) bool {
	return isLetterRune(r) || isDigitRune(r) || isMarkRune(r) || r == '_'
}

// FindWordBackward returns the cursor position after moving one word backward.
// Cursors and results are UTF-16 code-unit offsets (upstream JS string
// offsets), so the caller converts from its own byte offsets.
func FindWordBackward(text string, cursor int) int {
	if cursor <= 0 {
		return 0
	}
	if limit := jsLength(text); cursor > limit {
		cursor = limit
	}
	byteCursor := byteOffsetForUTF16Index(text, cursor)
	segments := WordSegments(text[:byteCursor])

	newCursor := cursor
	// Skip trailing whitespace.
	for len(segments) > 0 && isWhitespaceString(segments[len(segments)-1].Segment) {
		newCursor -= jsLength(segments[len(segments)-1].Segment)
		segments = segments[:len(segments)-1]
	}
	if len(segments) == 0 {
		return newCursor
	}

	last := segments[len(segments)-1]
	if last.IsWordLike {
		// Skip inside one word-like segment, preserving ASCII punctuation
		// boundaries.
		lastPunctuationEnd := -1
		units := 0
		for _, r := range last.Segment {
			if punctuationChars[r] {
				lastPunctuationEnd = units
			}
			units += jsLength(string(r))
		}
		if lastPunctuationEnd < 0 {
			newCursor -= jsLength(last.Segment)
		} else {
			newCursor -= jsLength(last.Segment) - (lastPunctuationEnd + 1)
		}
		return newCursor
	}

	// Skip the non-word non-whitespace run (punctuation).
	for len(segments) > 0 {
		last := segments[len(segments)-1]
		if last.IsWordLike || isWhitespaceString(last.Segment) {
			break
		}
		newCursor -= jsLength(last.Segment)
		segments = segments[:len(segments)-1]
	}
	return newCursor
}

// FindWordForward returns the cursor position after moving one word forward.
// Cursors and results are UTF-16 code-unit offsets (upstream JS string
// offsets), so the caller converts from its own byte offsets.
func FindWordForward(text string, cursor int) int {
	limit := jsLength(text)
	if cursor >= limit {
		return limit
	}
	byteCursor := byteOffsetForUTF16Index(text, cursor)
	segments := WordSegments(text[byteCursor:])

	newCursor := cursor
	index := 0
	// Skip leading whitespace.
	for index < len(segments) && isWhitespaceString(segments[index].Segment) {
		newCursor += jsLength(segments[index].Segment)
		index++
	}
	if index >= len(segments) {
		return newCursor
	}

	next := segments[index]
	if next.IsWordLike {
		// Stop at the first ASCII punctuation inside the word-like segment.
		stop := jsLength(next.Segment)
		units := 0
		for _, r := range next.Segment {
			if punctuationChars[r] {
				stop = units
				break
			}
			units += jsLength(string(r))
		}
		newCursor += stop
		return newCursor
	}

	// Skip the non-word non-whitespace run (punctuation).
	for index < len(segments) {
		current := segments[index]
		if current.IsWordLike || isWhitespaceString(current.Segment) {
			break
		}
		newCursor += jsLength(current.Segment)
		index++
	}
	return newCursor
}

// isWhitespaceString reports whether every rune is whitespace (upstream
// isWhitespaceChar tests a single character; segment lengths are 1 here).
func isWhitespaceString(text string) bool {
	for _, r := range text {
		if !isWhitespaceRune(r) {
			return false
		}
	}
	return len(text) > 0
}
