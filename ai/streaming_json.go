package ai

import (
	"cmp"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"
)

// streamingJSONParser ports pi v1.5.0 (f9a26365)
// packages/ai/src/utils/streaming-json.ts StreamingJsonParser: an incremental,
// tolerant reader of JSON text that arrives in pieces. It follows the
// reference's rules for incomplete and malformed text (slots, phases, the
// tolerated missing commas and colons, held-back escapes, pending scalars),
// but instead of building values it writes the canonical JSON.stringify bytes
// of the root object into an append-only buffer as the text is read. A
// snapshot is that buffer plus the closing tail of the open path, so it costs
// a copy instead of a re-parse (D353).
//
// Only an object root is rendered: ToolCallArgumentsStream shows any other
// root as no arguments yet. JSON.parse orders two cases differently from text
// order: a duplicate key keeps its first position, and an array-index key
// ("0", "12") sorts before the other keys, ascending. An object that receives
// such a key stops being contiguous in canonical order: its members stay in
// arrival order in the buffer, a snapshot emits them in JS order, and closing
// the object rewrites its bytes in that order once (A424).
type streamingJSONParser struct {
	out   []byte
	stack []sjFrame
	// attached counts the frames at the bottom of the stack whose contents are
	// rendered: the root object and the containers placed inside it. A frame
	// above them is in an invalid position (or under a non-object root) and is
	// read but dropped.
	attached int

	rootSet    bool
	rootObject bool
	done       bool
	// loose records that the text left strict JSON: a tolerated missing comma
	// or colon, a dropped value, an invalid escape, a raw control character, a
	// lone surrogate, or text after the root. Until then the buffer of a closed
	// root object is exactly JSON.stringify(JSON.parse(text)).
	loose bool

	inString bool
	str      sjString

	scalar     []byte
	scalarSlot sjSlot
	inScalar   bool

	// partialRune holds the bytes of a UTF-8 sequence split across pieces.
	partialRune []byte

	// unordered counts the attached objects whose members are in arrival
	// order in out rather than canonical order.
	unordered int
	scratch   []byte

	suffix     []byte
	renderedAt int64 // sjNow of the last large render
	dirty      bool
	snapshot   json.RawMessage
}

type sjSlot uint8

const (
	sjNoSlot sjSlot = iota // an invalid position: the value is read but dropped
	sjRootSlot
	sjMemberSlot
	sjElementSlot
)

type sjPhase uint8

const (
	sjKeyPhase sjPhase = iota
	sjColonPhase
	sjValuePhase
	sjCommaPhase
)

type sjMember struct {
	// key is the member name's canonical encoding without quotes, which is
	// injective, so it also serves as the member's identity.
	key string
	// start and end bound the member's latest `"key":value` bytes in out,
	// without a separator.
	start, end int
	num        uint64 // the array index of an index key
	isIndex    bool
}

type sjFrame struct {
	array  bool
	phase  sjPhase
	key    string
	hasKey bool
	count  int // elements placed (arrays)
	// afterComma marks a separator with no member or element after it yet.
	afterComma bool
	start      int // offset of the opening bracket in out (attached frames)

	// Attached objects: one entry per distinct key, in first-arrival order.
	// While the frame is ordered this is also JS enumeration order (index
	// keys ascending, then named keys) and the members are contiguous in out.
	// An unordered frame renders index members by order, then named ones.
	members    []sjMember
	index      map[string]int
	named      int // members with a non-index key
	open       bool
	openMember int // the member whose value is being read, while open
	unordered  bool
	order      []int // unordered: index members by ascending num
	orderStale bool
}

type sjString struct {
	key      bool // a member name rather than a value
	attached bool // a value rendered into out
	slotted  bool // a value with a slot, which marks the snapshot stale as it grows
	keyBuf   []byte
	escape   [6]byte
	escLen   int
	high     uint16 // a decoded high surrogate waiting for its low half
}

var (
	// sjNow is a monotonic clock in nanoseconds; tests replace it.
	sjNow   = func() int64 { return int64(time.Since(sjEpoch)) }
	sjEpoch = time.Now()
)

const (
	// An unterminated number longer than this is not shown until it ends.
	sjMaxPendingScalar = 64
	// Objects switch from a linear key scan to a map at this many members.
	sjIndexThreshold = 16
	// Previews up to this size refresh at every delta.
	sjFreshBytes = 1 << 10
	// A larger preview is at most one frame (60 Hz) behind the text.
	sjFrameNanos = int64(16 * time.Millisecond)
	// Deeper text takes the exact parser, whose decoder limits nesting.
	sjMaxStrictDepth = 10000
)

// append reads the next piece of text in time linear in its length.
func (p *streamingJSONParser) append(text string) {
	index := 0
	for index < len(text) && !p.done {
		switch {
		case p.inString:
			index = p.readString(text, index)
		case p.inScalar:
			index = p.readScalar(text, index)
		default:
			p.structural(text[index])
			index++
		}
	}
	if p.done && !p.loose {
		for ; index < len(text); index++ {
			switch text[index] {
			case ' ', '\t', '\n', '\r':
			default:
				p.loose = true
				return
			}
		}
	}
}

// strictObject returns the canonical bytes of the complete text when it was
// one strict JSON object, the value ParseToolCallArguments would return.
func (p *streamingJSONParser) strictObject() (json.RawMessage, bool) {
	if !p.done || !p.rootObject || p.loose {
		return nil, false
	}
	return p.out, true
}

// snap returns the arguments preview of the text read so far: the root
// object's canonical bytes, or {} before an object root starts. It returns
// the previous preview while nothing visible changed or while a fresh copy is
// not yet paid for.
func (p *streamingJSONParser) snap() json.RawMessage {
	if p.snapshot != nil && !p.dirty {
		return p.snapshot
	}
	if !p.rootObject {
		p.dirty = false
		if p.snapshot == nil {
			p.snapshot = json.RawMessage(`{}`)
		}
		return p.snapshot
	}
	// A snapshot copies the whole buffer, so a preview above sjFreshBytes
	// refreshes at most once a frame; a closed root is always current.
	if p.snapshot != nil && !p.done && len(p.out) > sjFreshBytes {
		now := sjNow()
		if now-p.renderedAt < sjFrameNanos {
			return p.snapshot
		}
		p.renderedAt = now
	}
	p.dirty = false
	p.snapshot = p.render()
	return p.snapshot
}

func (p *streamingJSONParser) top() *sjFrame {
	if len(p.stack) == 0 {
		return nil
	}
	return &p.stack[len(p.stack)-1]
}

// topAttached reports whether the innermost open container is rendered.
func (p *streamingJSONParser) topAttached() bool {
	return len(p.stack) > 0 && p.attached == len(p.stack)
}

func (p *streamingJSONParser) structural(c byte) {
	switch c {
	case ' ', '\t', '\n', '\r':
	case '"':
		p.startString()
	case '{', '[':
		p.openContainer(c == '[')
	case '}', ']':
		p.closeContainer(c)
	case ':':
		if frame := p.top(); frame != nil && !frame.array && frame.phase == sjColonPhase {
			frame.phase = sjValuePhase
		} else {
			p.loose = true
		}
	case ',':
		if frame := p.top(); frame != nil {
			if frame.phase != sjCommaPhase {
				p.loose = true
			}
			frame.afterComma = true
			if frame.array {
				frame.phase = sjValuePhase
			} else {
				frame.phase = sjKeyPhase
			}
		} else {
			p.loose = true
		}
	default:
		p.inScalar = true
		p.scalar = append(p.scalar[:0], c)
		p.scalarSlot = p.valueSlot()
		p.checkValueStart(p.scalarSlot)
		p.markScalarDirty()
	}
}

// valueSlot is where a value starting now goes; sjNoSlot when this position
// takes no value.
func (p *streamingJSONParser) valueSlot() sjSlot {
	frame := p.top()
	if frame == nil {
		if p.rootSet {
			return sjNoSlot
		}
		return sjRootSlot
	}
	if frame.array {
		return sjElementSlot
	}
	// A missing colon is tolerated: {"a" 1} reads as {"a": 1}.
	if (frame.phase == sjValuePhase || frame.phase == sjColonPhase) && frame.hasKey {
		return sjMemberSlot
	}
	return sjNoSlot
}

// checkValueStart records a value that strict JSON does not allow here: one
// in an invalid position, after a missing colon or a missing comma.
func (p *streamingJSONParser) checkValueStart(slot sjSlot) {
	frame := p.top()
	switch {
	case slot == sjNoSlot:
		p.loose = true
	case frame != nil:
		if frame.phase != sjValuePhase {
			p.loose = true
		}
		frame.afterComma = false
	}
}

// placed reports whether a value at slot in the innermost frame is rendered.
func (p *streamingJSONParser) placedAttached(slot sjSlot) bool {
	return (slot == sjMemberSlot || slot == sjElementSlot) && p.topAttached()
}

func (p *streamingJSONParser) startString() {
	frame := p.top()
	// A missing comma is tolerated: {"a": 1 "b": 2} reads the second member.
	if frame != nil && !frame.array && (frame.phase == sjKeyPhase || frame.phase == sjCommaPhase) {
		if frame.phase == sjCommaPhase {
			p.loose = true
		}
		frame.afterComma = false
		p.inString = true
		p.str = sjString{key: true, keyBuf: p.str.keyBuf[:0]}
		return
	}
	slot := p.valueSlot()
	p.checkValueStart(slot)
	p.inString = true
	p.str = sjString{keyBuf: p.str.keyBuf[:0], slotted: slot != sjNoSlot}
	p.dirty = p.dirty || slot != sjNoSlot
	switch {
	case slot == sjRootSlot:
		p.rootSet = true
	case p.placedAttached(slot):
		p.str.attached = true
		p.beginValue(slot)
		p.out = append(p.out, '"')
		p.dirty = true
	case slot == sjElementSlot:
		frame.count++
	}
}

// readString reads string characters from start until the string ends or
// the text does, and returns the next index. Each run up to the next quote
// or backslash is scanned once; a run of printable ASCII is copied as is.
func (p *streamingJSONParser) readString(text string, start int) int {
	s := &p.str
	for index := start; index < len(text); {
		if s.escLen > 0 && p.continueEscape(text[index]) {
			index++
			continue
		}
		// A character that did not continue the escape is read normally.
		runStart := index
		class := byte(0)
	scan:
		for index < len(text) {
			// Skip eight printable ASCII bytes at a time.
			for index+8 <= len(text) && !sjSpecialWord(loadWord(text[index:index+8])) {
				index += 8
			}
			for stop := min(index+8, len(text)); index < stop; index++ {
				c := text[index]
				if c == '"' || c == '\\' {
					break scan
				}
				class |= sjByteClass[c]
			}
		}
		if class&sjControlByte != 0 {
			p.loose = true
		}
		p.appendRun(text[runStart:index], class == 0)
		if index == len(text) {
			break
		}
		if text[index] == '"' {
			p.endString()
			return index + 1
		}
		s.escape[0] = '\\'
		s.escLen = 1
		index++
	}
	return len(text)
}

func loadWord(b string) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// sjSpecialWord reports whether any of eight bytes is a quote, a backslash,
// a control character or non-ASCII.
func sjSpecialWord(w uint64) bool {
	const lsb, msb = 0x0101010101010101, 0x8080808080808080
	control := (w - 0x20*lsb) &^ w
	quote := w ^ '"'*lsb
	backslash := w ^ '\\'*lsb
	return (control|(quote-lsb)&^quote|(backslash-lsb)&^backslash|w)&msb != 0
}

const (
	sjControlByte = 1 << iota
	sjNonASCIIByte
)

// sjByteClass marks the string bytes that need escaping or UTF-8 handling.
var sjByteClass = func() (table [256]byte) {
	for c := range 0x20 {
		table[c] = sjControlByte
	}
	for c := 0x80; c < 0x100; c++ {
		table[c] = sjNonASCIIByte
	}
	return table
}()

// continueEscape continues an escape with c; false when c is not part of it
// and must be read normally.
func (p *streamingJSONParser) continueEscape(c byte) bool {
	s := &p.str
	if s.escLen == 1 {
		var decoded byte
		switch c {
		case '"', '\\', '/':
			decoded = c
		case 'b':
			decoded = '\b'
		case 'f':
			decoded = '\f'
		case 'n':
			decoded = '\n'
		case 'r':
			decoded = '\r'
		case 't':
			decoded = '\t'
		case 'u':
			s.escape[1] = 'u'
			s.escLen = 2
			return true
		default:
			// Invalid escape: keep the backslash as text and read c normally, as repairJson() does.
			p.loose = true
			s.escLen = 0
			p.appendRun(`\`, false)
			return false
		}
		s.escLen = 0
		p.appendUnit(uint16(decoded))
		return true
	}
	if isHexByte(c) {
		s.escape[s.escLen] = c
		s.escLen++
		if s.escLen < 6 {
			return true
		}
		value, _ := strconv.ParseUint(string(s.escape[2:6]), 16, 16)
		s.escLen = 0
		p.appendUnit(uint16(value))
		return true
	}
	// \u without four hex digits: keep it as text and read c normally, as repairJson() does.
	p.loose = true
	literal := string(s.escape[:s.escLen])
	s.escLen = 0
	p.appendRun(literal, false)
	return false
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// stringSink is where the open string's encoded text goes, or nil when the
// string is dropped.
func (p *streamingJSONParser) stringSink() *[]byte {
	switch {
	case p.str.key:
		return &p.str.keyBuf
	case p.str.attached:
		p.dirty = true
		return &p.out
	}
	p.dirty = p.dirty || p.str.slotted
	return nil
}

// appendUnit appends one decoded UTF-16 code unit, pairing surrogates the
// way a JavaScript string does; JSON.stringify writes a lone one as \udxxx.
func (p *streamingJSONParser) appendUnit(unit uint16) {
	dst := p.stringSink()
	if dst == nil {
		return
	}
	s := &p.str
	p.flushPartial(dst)
	switch {
	case unit >= 0xd800 && unit < 0xdc00:
		p.flushHigh(dst)
		s.high = unit
	case unit >= 0xdc00 && unit < 0xe000:
		if s.high != 0 {
			r := utf16Pair(s.high, unit)
			s.high = 0
			*dst = utf8.AppendRune(*dst, r)
			return
		}
		p.loose = true
		*dst = appendLoneSurrogate(*dst, unit)
	default:
		p.flushHigh(dst)
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], rune(unit))
		*dst = appendJSONStringContent(*dst, string(buf[:n]))
	}
}

func utf16Pair(high, low uint16) rune {
	return (rune(high)-0xd800)<<10 + (rune(low) - 0xdc00) + 0x10000
}

func appendLoneSurrogate(dst []byte, unit uint16) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '\\', 'u', hex[unit>>12], hex[unit>>8&0xf], hex[unit>>4&0xf], hex[unit&0xf])
}

func (p *streamingJSONParser) flushHigh(dst *[]byte) {
	if p.str.high != 0 {
		p.loose = true
		*dst = appendLoneSurrogate(*dst, p.str.high)
		p.str.high = 0
	}
}

// appendRun appends raw string text, holding back a UTF-8 sequence that the
// piece split so the next piece completes it. plain reports a run of
// printable ASCII without quotes or backslashes, which needs no escaping.
func (p *streamingJSONParser) appendRun(run string, plain bool) {
	if run == "" {
		return
	}
	dst := p.stringSink()
	if dst == nil {
		return
	}
	p.flushHigh(dst)
	if plain && len(p.partialRune) == 0 {
		*dst = append(*dst, run...)
		return
	}
	if len(p.partialRune) > 0 {
		run = string(p.partialRune) + run
		p.partialRune = p.partialRune[:0]
	}
	if cut := incompleteRuneTail(run); cut < len(run) {
		p.partialRune = append(p.partialRune, run[cut:]...)
		run = run[:cut]
	}
	*dst = appendJSONStringContent(*dst, run)
}

// flushPartial writes a held UTF-8 fragment that no continuation completed;
// it encodes as U+FFFD.
func (p *streamingJSONParser) flushPartial(dst *[]byte) {
	if len(p.partialRune) > 0 {
		*dst = appendJSONStringContent(*dst, string(p.partialRune))
		p.partialRune = p.partialRune[:0]
	}
}

// utf8RuneLen is the sequence length a UTF-8 leading byte announces.
func utf8RuneLen(lead byte) int {
	switch {
	case lead >= 0xf0:
		return 4
	case lead >= 0xe0:
		return 3
	case lead >= 0xc0:
		return 2
	}
	return 1
}

// incompleteRuneTail returns where an incomplete trailing UTF-8 sequence
// starts, or len(s) when s ends on a rune boundary.
func incompleteRuneTail(s string) int {
	for back := 1; back <= 3 && back <= len(s); back++ {
		c := s[len(s)-back]
		if c&0xc0 == 0x80 {
			continue
		}
		if c >= 0xc0 && utf8RuneLen(c) > back {
			return len(s) - back
		}
		return len(s)
	}
	return len(s)
}

// appendJSONStringContent writes text the way JSON.stringify does inside the
// quotes (see appendCanonicalJSONString).
func appendJSONStringContent(dst []byte, text string) []byte {
	const hex = "0123456789abcdef"
	run := 0
	for index := 0; index < len(text); {
		c := text[index]
		if c >= 0x20 && c < utf8.RuneSelf && c != '"' && c != '\\' {
			index++
			continue
		}
		dst = append(dst, text[run:index]...)
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			if c < 0x20 {
				dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0x0f])
				break
			}
			r, size := utf8.DecodeRuneInString(text[index:])
			if r == utf8.RuneError && size == 1 {
				dst = utf8.AppendRune(dst, utf8.RuneError)
			} else {
				dst = append(dst, text[index:index+size]...)
			}
			index += size
			run = index
			continue
		}
		index++
		run = index
	}
	return append(dst, text[run:]...)
}

func (p *streamingJSONParser) endString() {
	s := &p.str
	p.inString = false
	if s.key {
		p.flushPartial(&s.keyBuf)
		p.flushHigh(&s.keyBuf)
		frame := p.top()
		frame.key = string(s.keyBuf)
		frame.hasKey = true
		frame.phase = sjColonPhase
		return
	}
	if s.attached {
		p.flushPartial(&p.out)
		p.flushHigh(&p.out)
		p.out = append(p.out, '"')
		p.dirty = true
	}
	p.partialRune = p.partialRune[:0]
	p.valueEnded()
}

// readScalar reads number or literal bytes up to the next delimiter and
// returns the delimiter's index.
func (p *streamingJSONParser) readScalar(text string, start int) int {
	end := start
	for end < len(text) && !isStreamingDelimiter(text[end]) {
		end++
	}
	if end > start {
		p.scalar = append(p.scalar, text[start:end]...)
		p.markScalarDirty()
	}
	if end < len(text) {
		p.endScalar()
	}
	return end
}

// markScalarDirty follows the reference: any scalar with a slot, rendered
// or not, marks the snapshot stale.
func (p *streamingJSONParser) markScalarDirty() {
	if p.scalarSlot != sjNoSlot {
		p.dirty = true
	}
}

func isStreamingDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ',', ':', '{', '}', '[', ']', '"':
		return true
	}
	return false
}

func (p *streamingJSONParser) endScalar() {
	slot := p.scalarSlot
	p.inScalar = false
	p.scalarSlot = sjNoSlot
	if value, ok := streamingScalarValue(p.scalar); ok {
		// Like the reference, only a placed value marks the snapshot stale: a
		// dropped token leaves the pending value it showed until the next change.
		p.dirty = p.dirty || slot != sjNoSlot
		switch {
		case slot == sjRootSlot:
			p.rootSet = true
		case p.placedAttached(slot):
			p.beginValue(slot)
			p.out = append(p.out, value...)
			p.dirty = true
		case slot == sjElementSlot:
			p.top().count++
		}
	} else {
		p.loose = true
	}
	p.valueEnded()
}

// streamingScalarValue is the encoded value of a complete number or literal
// token; false for anything else, which is dropped.
func streamingScalarValue(token []byte) (string, bool) {
	switch string(token) {
	case "true", "false", "null":
		return string(token), true
	}
	if !isJSONNumberToken(token) {
		return "", false
	}
	return encodeJSNumberToken(token), true
}

// pendingScalarValue is the encoded value an unterminated token shows.
func pendingScalarValue(token []byte) (string, bool) {
	for _, literal := range [...]string{"true", "false", "null"} {
		if len(token) <= len(literal) && literal[:len(token)] == string(token) {
			return literal, true
		}
	}
	if isJSONNumberToken(token) {
		return encodeJSNumberToken(token), true
	}
	// An exponent without digits is ignored: 1e and 1e+ show 1.
	end := len(token)
	if end > 0 && (token[end-1] == '+' || token[end-1] == '-') {
		end--
	}
	if end > 0 && (token[end-1] == 'e' || token[end-1] == 'E') && isJSONNumberToken(token[:end-1]) && !hasExponent(token[:end-1]) {
		return encodeJSNumberToken(token[:end-1]), true
	}
	return "", false
}

func hasExponent(token []byte) bool {
	for _, c := range token {
		if c == 'e' || c == 'E' {
			return true
		}
	}
	return false
}

// isJSONNumberToken matches -?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?.
func isJSONNumberToken(token []byte) bool {
	i := 0
	if i < len(token) && token[i] == '-' {
		i++
	}
	digits := func() int {
		start := i
		for i < len(token) && token[i] >= '0' && token[i] <= '9' {
			i++
		}
		return i - start
	}
	if i < len(token) && token[i] == '0' {
		i++
	} else if digits() == 0 {
		return false
	}
	if i < len(token) && token[i] == '.' {
		i++
		if digits() == 0 {
			return false
		}
	}
	if i < len(token) && (token[i] == 'e' || token[i] == 'E') {
		i++
		if i < len(token) && (token[i] == '+' || token[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == len(token)
}

// encodeJSNumberToken is JSON.stringify(Number(token)) for a JSON number.
func encodeJSNumberToken(token []byte) string {
	value, _ := strconv.ParseFloat(string(token), 64)
	if math.IsInf(value, 0) {
		return "null"
	}
	if value == 0 {
		return "0"
	}
	return JSNumberToString(value)
}

func (p *streamingJSONParser) openContainer(array bool) {
	slot := p.valueSlot()
	p.checkValueStart(slot)
	if len(p.stack) >= sjMaxStrictDepth {
		p.loose = true
	}
	attached := false
	switch {
	case slot == sjRootSlot:
		p.rootSet = true
		if !array {
			p.rootObject = true
			attached = true
		}
	case p.placedAttached(slot):
		attached = true
		p.beginValue(slot)
	case slot == sjElementSlot:
		p.top().count++
	}
	p.dirty = true
	frame := sjFrame{array: array, phase: sjKeyPhase}
	if array {
		frame.phase = sjValuePhase
	}
	if attached {
		frame.start = len(p.out)
		if array {
			p.out = append(p.out, '[')
		} else {
			p.out = append(p.out, '{')
		}
		p.attached++
		p.dirty = true
	}
	p.stack = append(p.stack, frame)
}

func (p *streamingJSONParser) closeContainer(closer byte) {
	// A stray closer outside any container is ignored.
	if len(p.stack) == 0 {
		p.loose = true
		return
	}
	frame := &p.stack[len(p.stack)-1]
	p.dirty = true
	if frame.array != (closer == ']') || frame.afterComma || !frame.array && (frame.phase == sjColonPhase || frame.phase == sjValuePhase) {
		p.loose = true
	}
	if p.topAttached() {
		switch {
		case frame.array:
			p.out = append(p.out, ']')
		case frame.unordered:
			// Once, at the close: the object's bytes in canonical order, so a
			// closed root is JSON.stringify's bytes again.
			p.scratch = p.emitFrame(p.scratch[:0], len(p.stack)-1)
			p.out = append(p.out[:frame.start], p.scratch...)
			p.unordered--
		default:
			p.out = append(p.out, '}')
		}
		p.attached--
		p.dirty = true
	}
	p.stack[len(p.stack)-1] = sjFrame{}
	p.stack = p.stack[:len(p.stack)-1]
	p.valueEnded()
}

// valueEnded handles the end of a value in the innermost frame, or of the
// root value when there is none.
func (p *streamingJSONParser) valueEnded() {
	frame := p.top()
	if frame == nil {
		// Text after the root value is ignored, but only once a root value exists.
		if p.rootSet {
			p.done = true
		}
		return
	}
	frame.phase = sjCommaPhase
	if frame.open {
		p.commitMember(frame)
	}
}

// beginValue writes the separator (and member name) for a value placed at
// slot in the innermost, attached frame. A member that JS orders before
// existing ones, or a duplicate key, turns the frame unordered: the member is
// still appended, and rendering places it.
func (p *streamingJSONParser) beginValue(slot sjSlot) {
	frame := p.top()
	if slot == sjElementSlot {
		if frame.count > 0 {
			p.out = append(p.out, ',')
		}
		frame.count++
		return
	}
	position, duplicate := frame.lookup(frame.key)
	num, isIndex := jsArrayIndexKey(frame.key)
	if !frame.unordered && (duplicate || isIndex && frame.named+len(frame.members) > 0 &&
		(frame.named > 0 || num < frame.members[len(frame.members)-1].num)) {
		frame.unordered = true
		frame.orderStale = true
		p.unordered++
	}
	if len(frame.members) > 0 {
		p.out = append(p.out, ',')
	}
	start := len(p.out)
	p.out = append(p.out, '"')
	p.out = append(p.out, frame.key...)
	p.out = append(p.out, '"', ':')
	if duplicate {
		frame.members[position].start = start
	} else {
		position = len(frame.members)
		frame.members = append(frame.members, sjMember{key: frame.key, start: start, num: num, isIndex: isIndex})
		if frame.index != nil {
			frame.index[frame.key] = position
		}
		if !isIndex {
			frame.named++
		} else if frame.unordered {
			frame.orderStale = true
		}
	}
	frame.openMember = position
	frame.open = true
}

// commitMember ends the open member.
func (p *streamingJSONParser) commitMember(frame *sjFrame) {
	frame.open = false
	frame.members[frame.openMember].end = len(p.out)
	if frame.index == nil && len(frame.members) >= sjIndexThreshold {
		frame.index = make(map[string]int, 2*len(frame.members))
		for i, member := range frame.members {
			frame.index[member.key] = i
		}
	}
}

// lookup finds key's member.
func (f *sjFrame) lookup(key string) (int, bool) {
	if f.index != nil {
		i, ok := f.index[key]
		return i, ok
	}
	for i := range f.members {
		if f.members[i].key == key {
			return i, true
		}
	}
	return 0, false
}

// indexPosition is where a new index key goes in an ordered frame, whose
// index members are an ascending prefix.
func (f *sjFrame) indexPosition(num uint64) int {
	low, high := 0, len(f.members)-f.named
	for low < high {
		mid := int(uint(low+high) >> 1)
		if f.members[mid].num < num {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low
}

// sortedOrder returns an unordered frame's index members by ascending index.
func (f *sjFrame) sortedOrder() []int {
	if f.orderStale {
		f.order = f.order[:0]
		for i := range f.members {
			if f.members[i].isIndex {
				f.order = append(f.order, i)
			}
		}
		slices.SortFunc(f.order, func(a, b int) int { return cmp.Compare(f.members[a].num, f.members[b].num) })
		f.orderStale = false
	}
	return f.order
}

// stale reports whether the last preview was throttled: the text changed it
// and no snapshot shows that yet.
func (p *streamingJSONParser) stale() bool {
	return p.dirty && p.snapshot != nil && p.rootObject
}

// refresh renders the current preview regardless of the frame gate, for a
// consumer that saw the stream pause.
func (p *streamingJSONParser) refresh() json.RawMessage {
	p.dirty = false
	p.snapshot = p.render()
	p.renderedAt = sjNow()
	return p.snapshot
}

// render builds a snapshot: the buffer, the pending scalar, and the closing
// tail of every open rendered container. The common case appends the small
// tail to a cap-limited view of the buffer, so growslice copies the buffer
// once and clears only its spare capacity.
func (p *streamingJSONParser) render() json.RawMessage {
	if p.unordered > 0 {
		out := p.emitFrame(make([]byte, 0, len(p.out)+len(p.suffix)+16), 0)
		return out[:len(out):len(out)]
	}
	from, to, member, placed := p.pendingPlacement()
	tail := p.suffix[:0]
	prefix := p.out
	if placed {
		prefix = p.out[:from]
		tail = append(tail, member...)
		tail = append(tail, p.out[to:]...)
	}
	tail = p.appendOpenString(tail)
	for i := p.attached - 1; i >= 0; i-- {
		tail = append(tail, closerOf(&p.stack[i]))
	}
	p.suffix = tail
	out := append(prefix[:len(prefix):len(prefix)], tail...)
	return out[:len(out):len(out)]
}

func closerOf(frame *sjFrame) byte {
	if frame.array {
		return ']'
	}
	return '}'
}

// appendOpenString closes an attached string value that is still streaming.
func (p *streamingJSONParser) appendOpenString(dst []byte) []byte {
	if p.inString && p.str.attached {
		if p.str.high != 0 {
			dst = appendLoneSurrogate(dst, p.str.high)
		}
		dst = append(dst, '"')
	}
	return dst
}

// pendingValue is the value an unterminated scalar shows in the innermost,
// attached frame; false when it shows nothing.
func (p *streamingJSONParser) pendingValue() (string, bool) {
	if !p.inScalar || !p.placedAttached(p.scalarSlot) || len(p.scalar) > sjMaxPendingScalar {
		return "", false
	}
	return pendingScalarValue(p.scalar)
}

// pendingPlacement places the unterminated scalar in an ordered innermost
// frame: it replaces out[from:to] (from == to for an insert) with member.
func (p *streamingJSONParser) pendingPlacement() (from, to int, member []byte, placed bool) {
	value, ok := p.pendingValue()
	if !ok {
		return 0, 0, nil, false
	}
	frame := p.top()
	end := len(p.out)
	if p.scalarSlot == sjElementSlot {
		if frame.count > 0 {
			member = append(member, ',')
		}
		return end, end, append(member, value...), true
	}
	position, duplicate := frame.lookup(frame.key)
	if !duplicate {
		position = len(frame.members)
		if num, isIndex := jsArrayIndexKey(frame.key); isIndex {
			position = frame.indexPosition(num)
		}
	}
	member = make([]byte, 0, len(frame.key)+len(value)+5)
	if position == len(frame.members) && len(frame.members) > 0 {
		member = append(member, ',')
	}
	member = appendMemberBytes(member, frame.key, value)
	switch {
	case position == len(frame.members):
		return end, end, member, true
	case duplicate:
		old := frame.members[position]
		return old.start, old.end, member, true
	default:
		at := frame.members[position].start
		return at, at, append(member, ','), true
	}
}

func appendMemberBytes(dst []byte, key, value string) []byte {
	dst = append(dst, '"')
	dst = append(dst, key...)
	dst = append(dst, '"', ':')
	return append(dst, value...)
}

// emitFrame appends attached frame i, with everything open inside it, to
// dst: the general render, used while any attached object is unordered.
func (p *streamingJSONParser) emitFrame(dst []byte, i int) []byte {
	frame := &p.stack[i]
	top := i == p.attached-1
	end := len(p.out)
	if !top {
		end = p.stack[i+1].start
	}
	if !frame.unordered {
		if top {
			if from, to, member, placed := p.pendingPlacement(); placed {
				dst = append(dst, p.out[frame.start:from]...)
				dst = append(dst, member...)
				dst = append(dst, p.out[to:end]...)
			} else {
				dst = append(dst, p.out[frame.start:end]...)
			}
			dst = p.appendOpenString(dst)
		} else {
			dst = append(dst, p.out[frame.start:end]...)
			dst = p.emitFrame(dst, i+1)
		}
		return append(dst, closerOf(frame))
	}

	// The pending scalar's member, if this is its frame.
	pendingAt, pendingNew := -1, false
	var pendingNum uint64
	var pendingIsIndex bool
	value, pending := "", false
	if top {
		value, pending = p.pendingValue()
	}
	if pending {
		if position, duplicate := frame.lookup(frame.key); duplicate {
			pendingAt = position
		} else {
			pendingNew = true
			pendingNum, pendingIsIndex = jsArrayIndexKey(frame.key)
		}
	}
	dst = append(dst, '{')
	first := true
	emit := func(j int) {
		if !first {
			dst = append(dst, ',')
		}
		first = false
		member := &frame.members[j]
		switch {
		case j == pendingAt:
			dst = appendMemberBytes(dst, member.key, value)
		case frame.open && j == frame.openMember:
			dst = append(dst, p.out[member.start:end]...)
			if top {
				dst = p.appendOpenString(dst)
			} else {
				dst = p.emitFrame(dst, i+1)
			}
		default:
			dst = append(dst, p.out[member.start:member.end]...)
		}
	}
	emitPending := func() {
		if !first {
			dst = append(dst, ',')
		}
		first = false
		dst = appendMemberBytes(dst, frame.key, value)
		pendingNew = false
	}
	for _, j := range frame.sortedOrder() {
		if pendingNew && pendingIsIndex && pendingNum < frame.members[j].num {
			emitPending()
		}
		emit(j)
	}
	if pendingNew && pendingIsIndex {
		emitPending()
	}
	for j := range frame.members {
		if !frame.members[j].isIndex {
			emit(j)
		}
	}
	if pendingNew {
		emitPending()
	}
	return append(dst, '}')
}
