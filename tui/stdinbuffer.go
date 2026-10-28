package tui

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Port of src/stdin-buffer.ts: buffers input and emits complete sequences,
// handling partial escape sequences that arrive across multiple chunks.
//
// Upstream works on UTF-16 code units; Go iterates runes, so astral-plane
// characters are emitted as a single sequence instead of surrogate halves
// (divergence D49).

const (
	stdinEscape              = "\x1b"
	defaultSequenceTimeoutMS = 50
	defaultEscapeTimeoutMS   = 10
	bracketedPasteStart      = "\x1b[200~"
	bracketedPasteEnd        = "\x1b[201~"
)

type seqStatus string

const (
	seqComplete   seqStatus = "complete"
	seqIncomplete seqStatus = "incomplete"
	seqNotEscape  seqStatus = "not-escape"
)

// isCompleteSequence reports whether data is a complete escape sequence or
// needs more input.
func isCompleteSequence(data string) seqStatus {
	if !startsWith(data, stdinEscape) {
		return seqNotEscape
	}
	if len(data) == 1 {
		return seqIncomplete
	}

	afterEsc := data[1:]

	switch {
	case startsWith(afterEsc, "["):
		// Old-style mouse sequence: ESC[M + 3 bytes = 6 total.
		if startsWith(afterEsc, "[M") {
			if len(data) >= 6 {
				return seqComplete
			}
			return seqIncomplete
		}
		return isCompleteCsiSequence(data)
	case startsWith(afterEsc, "]"):
		return isCompleteOscSequence(data)
	case startsWith(afterEsc, "P"):
		return isCompleteDcsSequence(data)
	case startsWith(afterEsc, "_"):
		return isCompleteApcSequence(data)
	case startsWith(afterEsc, "O"):
		// ESC O followed by a single character.
		if len(afterEsc) >= 2 {
			return seqComplete
		}
		return seqIncomplete
	case len(afterEsc) == 1:
		// Meta key sequence: ESC followed by a single character.
		return seqComplete
	}
	// Unknown escape sequence: treat as complete.
	return seqComplete
}

func isCompleteCsiSequence(data string) seqStatus {
	if !startsWith(data, stdinEscape+"[") {
		return seqComplete
	}
	if len(data) < 3 {
		return seqIncomplete
	}

	payload := data[2:]
	// CSI sequences end with a byte in the range 0x40-0x7E (@-~).
	lastChar := payload[len(payload)-1]
	if lastChar >= 0x40 && lastChar <= 0x7e {
		// Special handling for SGR mouse sequences: ESC[<B;X;YMm.
		if startsWith(payload, "<") {
			if sgrMouseRegex.MatchString(payload) {
				return seqComplete
			}
			if lastChar == 'M' || lastChar == 'm' {
				parts := splitOn(payload[1:len(payload)-1], ";")
				if len(parts) == 3 && allDigits(parts) {
					return seqComplete
				}
			}
			return seqIncomplete
		}
		return seqComplete
	}
	return seqIncomplete
}

func isCompleteOscSequence(data string) seqStatus {
	if !startsWith(data, stdinEscape+"]") {
		return seqComplete
	}
	if endsWith(data, stdinEscape+"\\") || endsWith(data, "\x07") {
		return seqComplete
	}
	return seqIncomplete
}

func isCompleteDcsSequence(data string) seqStatus {
	if !startsWith(data, stdinEscape+"P") {
		return seqComplete
	}
	if endsWith(data, stdinEscape+"\\") {
		return seqComplete
	}
	return seqIncomplete
}

func isCompleteApcSequence(data string) seqStatus {
	if !startsWith(data, stdinEscape+"_") {
		return seqComplete
	}
	if endsWith(data, stdinEscape+"\\") {
		return seqComplete
	}
	return seqIncomplete
}

var (
	sgrMouseRegex            = regexp.MustCompile(`^<\d+;\d+;\d+[Mm]$`)
	terminalReplyHeaderRegex = regexp.MustCompile(`^\x1b(?:\](?:1[01];|4;\d{1,3};|7501;\?)|P>\||_G(?:[a-z]=\d+,)*[a-z]=\d+;)`)
	kittyPrintableRegex      = regexp.MustCompile(`^\x1b\[(\d+)(?::\d*)?(?::\d+)?u$`)
	// mouseReportHeadRegex includes ESC[ and each valid incomplete SGR field.
	mouseReportHeadRegex = regexp.MustCompile(`^\x1b(\[(<(\d+(;\d*){0,2})?)?)?$`)
	// mouseReportPrefixRegex allows other reports or keys after the completed tail.
	mouseReportPrefixRegex = regexp.MustCompile(`^\x1b\[<\d+;\d+;\d+[Mm]`)
)

// extractCompleteSequences splits the accumulated buffer into complete
// sequences plus the trailing remainder.
func extractCompleteSequences(buffer string) (sequences []string, remainder string) {
	pos := 0

	for pos < len(buffer) {
		remaining := buffer[pos:]

		if startsWith(remaining, stdinEscape) {
			terminalReply := terminalReplyHeaderRegex.MatchString(remaining)
			// Find the end of this escape sequence.
			seqEnd := 1
			for seqEnd <= len(remaining) {
				if terminalReply && seqEnd > 2 {
					char := remaining[seqEnd-1]
					if char == '\x18' || char == '\x1a' {
						pos += seqEnd
						break
					}
					if char == stdinEscape[0] && seqEnd < len(remaining) && remaining[seqEnd] != 0x5c {
						pos += seqEnd - 1
						break
					}
				}
				candidate := remaining[:seqEnd]
				status := isCompleteSequence(candidate)

				if status == seqComplete {
					// WezTerm with enable_kitty_keyboard sends the Escape key
					// press as a raw '\x1b' byte and the release as a full
					// Kitty CSI-u sequence, concatenated as
					// '\x1b\x1b[27;...u'. '\x1b\x1b' would normally parse as a
					// complete meta-key sequence, leaving '[27;...u' to be
					// typed as plain text. If the character immediately
					// following would begin a new escape sequence, emit only
					// the first ESC and restart from the second.
					if candidate == "\x1b\x1b" {
						nextChar := byte(0)
						if seqEnd < len(remaining) {
							nextChar = remaining[seqEnd]
						}
						if nextChar == '[' || nextChar == ']' || nextChar == 'O' || nextChar == 'P' || nextChar == '_' {
							sequences = append(sequences, stdinEscape)
							pos++
							break
						}
					}
					sequences = append(sequences, candidate)
					pos += seqEnd
					break
				} else if status == seqIncomplete {
					// Advance a character, not a byte: ESC + a Unicode key is
					// complete only after the whole UTF-8 rune is present.
					_, size := utf8.DecodeRuneInString(remaining[seqEnd:])
					seqEnd += max(size, 1)
				} else {
					// Should not happen when starting with ESC.
					sequences = append(sequences, candidate)
					pos += seqEnd
					break
				}
			}

			if seqEnd > len(remaining) {
				return sequences, remaining
			}
		} else {
			// Not an escape sequence: take a single rune.
			_, size := decodeRune(remaining)
			sequences = append(sequences, remaining[:size])
			pos += size
		}
	}

	return sequences, ""
}

// parseUnmodifiedKittyPrintableCodepoint extracts the codepoint from an
// unmodified Kitty printable-key sequence.
func parseUnmodifiedKittyPrintableCodepoint(sequence string) (int, bool) {
	match := kittyPrintableRegex.FindStringSubmatch(sequence)
	if match == nil {
		return 0, false
	}
	codepoint := parseInt(match[1])
	if codepoint >= 32 {
		return codepoint, true
	}
	return 0, false
}

// StdinBuffer buffers stdin input and emits complete sequences. Upstream
// extends EventEmitter; Go takes callbacks (divergence D51).
//
// The buffer is owned by the terminal's input consumer (the UI loop in
// interactive mode), which drives flushing through the deadline accessors —
// no mutex and no internal timer. PendingTimeout reports when an incomplete
// sequence must be force-flushed; FlushExpired flushes it once the deadline
// has passed.
type StdinBuffer struct {
	OnData  func(sequence string)
	OnPaste func(content string)

	buffer          string
	utf8Pending     string // terminal bytes awaiting a complete UTF-8 character
	pendingDeadline time.Time
	timeoutMS       int
	escapeTimeoutMS int
	pasteMode       bool
	pasteBuffer     *strings.Builder
	// pasteSearchFrom is the earliest byte not ruled out as a terminator start,
	// preserving a split marker across input chunks.
	pasteSearchFrom int
	// flushedHead holds what a deadline flush just released when it could still
	// be an SGR mouse head. Late tails may arrive in several reads or share a
	// read with more reports; reassemble them before dispatch (D169).
	flushedHead         string
	pendingCodepoint    int
	hasPendingCodepoint bool
}

// StdinBufferOptions configures a StdinBuffer.
type StdinBufferOptions struct {
	// Timeout is the maximum time to wait for an incomplete sequence such as
	// CSI or mouse (default 50ms).
	Timeout int
	// EscapeTimeout is the maximum time to wait after a lone ESC before
	// treating it as Escape (default 10ms). Increase for high-latency Alt+key
	// input (SSH).
	EscapeTimeout int
}

// NewStdinBuffer creates a buffer with the given options.
func NewStdinBuffer(options StdinBufferOptions) *StdinBuffer {
	timeoutMS := defaultSequenceTimeoutMS
	if options.Timeout != 0 {
		timeoutMS = options.Timeout
	}
	escapeTimeoutMS := defaultEscapeTimeoutMS
	if options.EscapeTimeout != 0 {
		escapeTimeoutMS = options.EscapeTimeout
	}
	return &StdinBuffer{timeoutMS: timeoutMS, escapeTimeoutMS: escapeTimeoutMS}
}

// Process feeds input data through the buffer.
func (b *StdinBuffer) Process(data []byte) {
	b.ProcessString(string(data))
}

// processUTF8Input mirrors terminal.ts stdin.setEncoding("utf8") before
// StdinBuffer receives text. Keep this separate from Process: its legacy raw
// Buffer API intentionally interprets a lone high byte as an Alt-key sequence.
// The terminal's input consumer owns both this decoder and sequence assembly.
func (b *StdinBuffer) processUTF8Input(data []byte) {
	input := b.utf8Pending + string(data)
	var decoded strings.Builder
	consumed := 0
	for consumed < len(input) && utf8.FullRuneInString(input[consumed:]) {
		codepoint, size := utf8.DecodeRuneInString(input[consumed:])
		decoded.WriteRune(codepoint)
		consumed += size
	}
	b.utf8Pending = input[consumed:]
	if decoded.Len() > 0 {
		b.ProcessString(decoded.String())
	}
}

// ProcessString feeds string input through the buffer.
func (b *StdinBuffer) ProcessString(data string) {
	b.process(data)
}

func (b *StdinBuffer) process(str string) {
	// D169: rejoin a timed-out head with its numeric tail, even when the
	// same read contains more wheel reports or keys. Parse the whole read normally.
	if b.flushedHead != "" {
		head := b.flushedHead
		b.flushedHead = ""
		joined := head + str
		if str != "" && (mouseReportPrefixRegex.MatchString(joined) || mouseReportHeadRegex.MatchString(joined)) {
			str = joined
		}
	}

	// High-byte conversion (for compatibility with parseKeypress): a single
	// byte > 127 becomes ESC + (byte - 128).
	if len(str) == 1 && str[0] > 127 {
		str = stdinEscape + string(rune(str[0]-128))
	}

	if str == "" && b.buffer == "" {
		b.emitDataSequence("")
		return
	}

	b.buffer += str

	if b.pasteMode {
		b.pasteBuffer.WriteString(b.buffer)
		b.buffer = ""
		b.finishPasteIfAvailable()
		return
	}

	if startIndex := indexOf(b.buffer, bracketedPasteStart); startIndex != -1 {
		if startIndex > 0 {
			beforePaste := b.buffer[:startIndex]
			sequences, _ := extractCompleteSequences(beforePaste)
			for _, sequence := range sequences {
				b.emitDataSequence(sequence)
			}
		}

		b.pendingCodepoint = 0
		b.hasPendingCodepoint = false
		content := b.buffer[startIndex+len(bracketedPasteStart):]
		b.buffer = ""
		if endIndex := indexOf(content, bracketedPasteEnd); endIndex != -1 {
			b.finishPaste(content[:endIndex], content[endIndex+len(bracketedPasteEnd):])
			return
		}

		b.pasteMode = true
		b.pasteBuffer = &strings.Builder{}
		b.pasteBuffer.WriteString(content)
		b.pasteSearchFrom = nextPasteSearchStart(len(content))
		return
	}

	sequences, remainder := extractCompleteSequences(b.buffer)
	b.buffer = remainder

	for _, sequence := range sequences {
		b.emitDataSequence(sequence)
	}

	// Incomplete sequences flush after the timeout; a lone ESC waits for the
	// (shorter) escape timeout. The consumer drives the flush through
	// FlushExpired once PendingTimeout passes.
	b.pendingDeadline = time.Time{}
	if b.buffer != "" {
		if terminalReplyHeaderRegex.MatchString(b.buffer) {
			return
		}
		timeoutMS := b.timeoutMS
		if b.buffer == stdinEscape {
			timeoutMS = b.escapeTimeoutMS
		}
		b.pendingDeadline = time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	}
}

// nextPasteSearchStart returns the earliest position not ruled out as the
// start of a terminator that may finish with future input.
func nextPasteSearchStart(contentLength int) int {
	return max(0, contentLength-len(bracketedPasteEnd)+1)
}

// finishPasteIfAvailable scans only the untested suffix, retaining enough
// overlap to find a terminator split across input chunks.
func (b *StdinBuffer) finishPasteIfAvailable() {
	content := b.pasteBuffer.String()
	searchFrom := b.pasteSearchFrom
	// All earlier positions were checked, except the suffix that could still
	// become the terminator after another chunk arrives.
	endOffset := indexOf(content[searchFrom:], bracketedPasteEnd)
	if endOffset == -1 {
		b.pasteSearchFrom = nextPasteSearchStart(len(content))
		return
	}

	endIndex := searchFrom + endOffset
	b.finishPaste(content[:endIndex], content[endIndex+len(bracketedPasteEnd):])
}

func (b *StdinBuffer) finishPaste(pastedContent string, remaining string) {
	b.pasteMode = false
	b.pasteBuffer = nil
	b.pasteSearchFrom = 0
	b.pendingCodepoint = 0
	b.hasPendingCodepoint = false
	b.emitPaste(pastedContent)
	if remaining != "" {
		b.process(remaining)
	}
}

// emitDataSequence deduplicates the raw character duplicate that
// terminals echo after an unmodified Kitty printable sequence.
func (b *StdinBuffer) emitDataSequence(sequence string) {
	if runeCount(sequence) == 1 {
		codepoint, _ := decodeRune(sequence)
		if b.hasPendingCodepoint && int(codepoint) == b.pendingCodepoint {
			b.pendingCodepoint = 0
			b.hasPendingCodepoint = false
			return
		}
	}

	if codepoint, ok := parseUnmodifiedKittyPrintableCodepoint(sequence); ok {
		b.pendingCodepoint = codepoint
		b.hasPendingCodepoint = true
	} else {
		b.pendingCodepoint = 0
		b.hasPendingCodepoint = false
	}
	b.emit(sequence)
}

func (b *StdinBuffer) emit(sequence string) {
	if b.OnData != nil {
		b.OnData(sequence)
	}
}

func (b *StdinBuffer) emitPaste(content string) {
	if b.OnPaste != nil {
		b.OnPaste(content)
	}
}

// PendingTimeout returns the deadline at which the buffered incomplete
// sequence must be force-flushed, and whether one is pending.
func (b *StdinBuffer) PendingTimeout() (time.Time, bool) {
	return b.pendingDeadline, !b.pendingDeadline.IsZero()
}

// FlushExpired flushes the buffered sequence once its deadline has passed
// (now >= deadline) and returns it; otherwise it returns nil.
func (b *StdinBuffer) FlushExpired(now time.Time) []string {
	if terminalReplyHeaderRegex.MatchString(b.buffer) {
		return nil
	}
	if b.pendingDeadline.IsZero() || now.Before(b.pendingDeadline) {
		return nil
	}
	b.pendingDeadline = time.Time{}
	if b.buffer == "" {
		return nil
	}
	sequences := []string{b.buffer}
	b.rememberFlushedHead(b.buffer)
	b.buffer = ""
	b.pendingCodepoint = 0
	b.hasPendingCodepoint = false
	return sequences
}

// rememberFlushedHead notes a flushed sequence that a following tail may
// complete (see flushedHead).
func (b *StdinBuffer) rememberFlushedHead(sequence string) {
	if mouseReportHeadRegex.MatchString(sequence) {
		b.flushedHead = sequence
	}
}

// Flush returns the buffered incomplete sequence, if any.
func (b *StdinBuffer) Flush() []string {
	b.pendingDeadline = time.Time{}
	if b.buffer == "" {
		return nil
	}
	sequences := []string{b.buffer}
	b.rememberFlushedHead(b.buffer)
	b.buffer = ""
	b.pendingCodepoint = 0
	b.hasPendingCodepoint = false
	return sequences
}

// Clear drops all buffered state.
func (b *StdinBuffer) Clear() {
	b.pendingDeadline = time.Time{}
	b.buffer = ""
	b.utf8Pending = ""
	b.flushedHead = ""
	b.pasteMode = false
	b.pasteBuffer = nil
	b.pasteSearchFrom = 0
	b.pendingCodepoint = 0
	b.hasPendingCodepoint = false
}

// GetBuffer returns the pending buffer contents.
func (b *StdinBuffer) GetBuffer() string {
	return b.buffer
}

// Destroy clears the buffer (upstream's destroy).
func (b *StdinBuffer) Destroy() { b.Clear() }

// ---- small helpers shared by the terminal port ----

func startsWith(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func splitOn(s, sep string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for {
		index := indexOf(s, sep)
		if index == -1 {
			out = append(out, s)
			return out
		}
		out = append(out, s[:index])
		s = s[index+len(sep):]
	}
}

func allDigits(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	return true
}

func parseInt(s string) int {
	value := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		value = value*10 + int(s[i]-'0')
	}
	return value
}

// decodeRune returns the first rune and its byte size.
func decodeRune(s string) (rune, int) {
	return utf8.DecodeRuneInString(s)
}

// runeCount counts the runes of a UTF-8 string. Upstream checks
// sequence.length === 1 (one UTF-16 code unit); runes are the Go analog
// (divergence D49).
func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}
