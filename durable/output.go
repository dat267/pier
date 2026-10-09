package durable

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

// Port of harness/output.ts: bounded running tool output and adaptive progress
// commits.

// OutputLimits are the retention limits of one tool's output.
type OutputLimits struct {
	MaxBytes int
	MaxLines int
	// Retain is RetainHead or RetainTail.
	Retain string
}

// Retention modes.
const (
	RetainHead = "head"
	RetainTail = "tail"
)

// BoundedOutput is retained output and what the limits dropped.
type BoundedOutput struct {
	Text         string
	DroppedBytes int
	DroppedLines int
}

// OutputSlice is an exact slice of the input within the limits, and what it
// left out.
type OutputSlice struct {
	Text         string
	Bytes        int
	DroppedBytes int
	DroppedLines int
}

const newlineByte = 0x0a

// SanitizeOutput removes control characters that break display and
// transcripts; tabs and newlines stay.
func SanitizeOutput(text string) string {
	if !strings.ContainsFunc(text, isInvalidOutputRune) {
		return text
	}
	var builder strings.Builder
	builder.Grow(len(text))
	for _, value := range text {
		if !isInvalidOutputRune(value) {
			builder.WriteRune(value)
		}
	}
	return builder.String()
}

// isInvalidOutputRune is upstream INVALID_OUTPUT: C0 controls except tab and
// newline, and the interlinear-annotation block.
func isInvalidOutputRune(value rune) bool {
	if value >= 0x00 && value <= 0x08 {
		return true
	}
	if value >= 0x0b && value <= 0x1f {
		return true
	}
	return value >= 0xfff9 && value <= 0xfffb
}

// BoundOutput bounds text to whole lines within the limits: the first lines for
// head, the last lines for tail. A single line longer than MaxBytes is cut at
// the byte limit on a character boundary.
func BoundOutput(text string, limits OutputLimits) OutputSlice {
	data := []byte(text)
	from, to := 0, len(data)
	if limits.Retain == RetainHead {
		from, to = headRange(data, limits)
	} else {
		from, to = tailRange(data, limits)
	}
	kept := data[from:to]
	result := OutputSlice{
		Bytes:        len(kept),
		DroppedBytes: len(data) - len(kept),
		DroppedLines: lineCountBytes(data) - lineCountBytes(kept),
	}
	if len(kept) == len(data) {
		result.Text = text
	} else {
		result.Text = string(kept)
	}
	return result
}

func headRange(data []byte, limits OutputLimits) (int, int) {
	if limits.MaxLines == 0 || limits.MaxBytes == 0 {
		return 0, 0
	}
	end := len(data)
	lines := 0
	for index := bytes.IndexByte(data, newlineByte); index != -1; index = indexByteFrom(data, index+1) {
		lines++
		if lines == limits.MaxLines {
			end = index + 1
			break
		}
	}
	if end > limits.MaxBytes {
		newline := lastIndexByteBefore(data, limits.MaxBytes-1)
		if newline == -1 {
			end = CharacterEnd(data, limits.MaxBytes)
		} else {
			end = newline + 1
		}
	}
	return 0, end
}

func tailRange(data []byte, limits OutputLimits) (int, int) {
	if limits.MaxLines == 0 || limits.MaxBytes == 0 {
		return len(data), len(data)
	}
	// A trailing newline ends the last line rather than starting another.
	last := len(data) - 1
	if len(data) > 0 && data[len(data)-1] == newlineByte {
		last = len(data) - 2
	}
	start := 0
	lines := 1
	index := lastIndexByteBefore(data, last)
	for index != -1 {
		if lines == limits.MaxLines {
			start = index + 1
			break
		}
		lines++
		if index == 0 {
			break
		}
		index = lastIndexByteBefore(data, index-1)
	}
	if len(data)-start > limits.MaxBytes {
		from := len(data) - limits.MaxBytes
		newline := indexByteFrom(data, from-1)
		if newline != -1 && newline+1 < len(data) {
			start = newline + 1
		} else {
			start = characterStart(data, from)
		}
	}
	return start, len(data)
}

// CharacterEnd is the last character boundary at or before index.
func CharacterEnd(data []byte, index int) int {
	end := index
	for end > 0 && byteAt(data, end)&0xc0 == 0x80 {
		end--
	}
	return end
}

func characterStart(data []byte, index int) int {
	start := index
	for start < len(data) && byteAt(data, start)&0xc0 == 0x80 {
		start++
	}
	return start
}

func byteAt(data []byte, index int) byte {
	if index < 0 || index >= len(data) {
		return 0
	}
	return data[index]
}

func indexByteFrom(data []byte, from int) int {
	if from < 0 {
		from = 0
	}
	if from > len(data) {
		return -1
	}
	index := bytes.IndexByte(data[from:], newlineByte)
	if index == -1 {
		return -1
	}
	return from + index
}

func lastIndexByteBefore(data []byte, atOrBefore int) int {
	if atOrBefore < 0 || len(data) == 0 {
		return -1
	}
	if atOrBefore >= len(data) {
		atOrBefore = len(data) - 1
	}
	return bytes.LastIndexByte(data[:atOrBefore+1], newlineByte)
}

func lineCountBytes(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	newlines := 0
	for index := bytes.IndexByte(data, newlineByte); index != -1; {
		newlines++
		index = indexByteFrom(data, index+1)
	}
	if data[len(data)-1] == newlineByte {
		return newlines
	}
	return newlines + 1
}

// OutputBuffer is the bounded running output of one tool call. Accepting a
// chunk costs time proportional to the chunk: head retention stops storing once
// the window is full, and tail retention drops text the window no longer needs
// when it snapshots. Whole-stream counts are kept so dropped totals stay exact.
type OutputBuffer struct {
	limits OutputLimits

	chunks          []outputChunk
	storedBytes     int
	storedNewlines  int
	full            bool
	totalBytes      int
	totalNewlines   int
	endsWithNewline bool
}

type outputChunk struct {
	text     string
	bytes    int
	newlines int
}

// NewOutputBuffer builds a buffer for one tool's output limits.
func NewOutputBuffer(limits OutputLimits) *OutputBuffer {
	return &OutputBuffer{limits: limits, endsWithNewline: true}
}

// StoredBytes is the bytes currently held; bounded by the limits plus one
// chunk.
func (b *OutputBuffer) StoredBytes() int { return b.storedBytes }

// Push accepts a chunk and any text the environment omitted immediately before it.
func (b *OutputBuffer) Push(text string, skipped ...*ShellOutputSkip) bool {
	if len(skipped) == 0 || skipped[0] == nil {
		return b.accept(text)
	}
	if b.limits.Retain != RetainTail {
		panic("Skipped output requires tail retention")
	}
	omitted := skipped[0]
	if omitted.Bytes > 0 {
		b.totalBytes += omitted.Bytes
		b.totalNewlines += omitted.Newlines
		b.endsWithNewline = omitted.EndsWithNewline
		b.chunks = nil
		b.storedBytes = 0
		b.storedNewlines = 0
	}
	b.accept(text)
	return true
}

// End flushes an incomplete trailing character; call when the stream ends.
func (b *OutputBuffer) End() { b.accept("") }

func (b *OutputBuffer) accept(text string) bool {
	if text == "" {
		return false
	}
	byteLength := UTF8ByteLength(text)
	newlines := countNewlines(text)
	b.totalBytes += byteLength
	b.totalNewlines += newlines
	b.endsWithNewline = strings.HasSuffix(text, "\n")
	if b.full {
		return true
	}
	b.chunks = append(b.chunks, outputChunk{text: text, bytes: byteLength, newlines: newlines})
	b.storedBytes += byteLength
	b.storedNewlines += newlines
	if b.limits.Retain == RetainHead {
		b.full = b.storedBytes > b.limits.MaxBytes || b.storedNewlines >= b.limits.MaxLines
		return true
	}
	for len(b.chunks) > 1 {
		first := b.chunks[0]
		bytesAfter := b.storedBytes - first.bytes
		newlinesAfter := b.storedNewlines - first.newlines
		if bytesAfter <= b.limits.MaxBytes+1 && newlinesAfter <= b.limits.MaxLines+1 {
			break
		}
		b.chunks = b.chunks[1:]
		b.storedBytes = bytesAfter
		b.storedNewlines = newlinesAfter
	}
	return true
}

// Snapshot is the retained, sanitized output and what the limits dropped from
// the whole stream.
func (b *OutputBuffer) Snapshot() BoundedOutput {
	var storedBuilder strings.Builder
	for _, chunk := range b.chunks {
		storedBuilder.WriteString(chunk.text)
	}
	stored := storedBuilder.String()
	kept := BoundOutput(stored, b.limits)
	storedLines := lineCountNumber(b.storedNewlines, stored == "" || strings.HasSuffix(stored, "\n"))
	keptLines := storedLines - kept.DroppedLines
	if b.limits.Retain == RetainTail || len(b.chunks) > 1 {
		if b.limits.Retain == RetainTail {
			text := tailMargin(stored, b.limits)
			bytes := UTF8ByteLength(text)
			if text == "" {
				b.chunks = nil
			} else {
				b.chunks = []outputChunk{{text: text, bytes: bytes, newlines: countNewlines(text)}}
			}
			b.storedBytes = bytes
			if len(b.chunks) > 0 {
				b.storedNewlines = b.chunks[0].newlines
			} else {
				b.storedNewlines = 0
			}
		} else {
			b.chunks = []outputChunk{{text: stored, bytes: b.storedBytes, newlines: countNewlines(stored)}}
			b.storedNewlines = b.chunks[0].newlines
		}
	}
	return BoundedOutput{
		Text:         SanitizeOutput(kept.Text),
		DroppedBytes: b.totalBytes - kept.Bytes,
		DroppedLines: lineCountNumber(b.totalNewlines, b.endsWithNewline) - keptLines,
	}
}

// tailMargin keeps enough context before the current tail to compute the next tail without the full stream.
func tailMargin(text string, limits OutputLimits) string {
	data := []byte(text)
	byteStart := 0
	if len(data) > limits.MaxBytes {
		byteStart = CharacterEnd(data, len(data)-limits.MaxBytes-1)
	}
	lineStart := 0
	newlines := 0
	for index := bytes.LastIndexByte(data, newlineByte); index != -1; index = bytes.LastIndexByte(data[:index], newlineByte) {
		newlines++
		if newlines > limits.MaxLines {
			lineStart = index
			break
		}
		if index == 0 {
			break
		}
	}
	start := max(byteStart, lineStart)
	return string(data[start:])
}

// lineCountNumber is lines of text with newlines newlines; a final unterminated
// line counts.
func lineCountNumber(newlines int, terminated bool) int {
	if terminated {
		return newlines
	}
	return newlines + 1
}

func countNewlines(text string) int {
	return strings.Count(text, "\n")
}

// Default adaptive progress pacing (upstream MIN_PROGRESS_INTERVAL_MS and
// PROGRESS_BYTES_PER_SECOND). Each Progress copies them, so a test can shorten
// its own instance without touching shared state.
const (
	defaultProgressMinIntervalMs  = 100
	defaultProgressBytesPerSecond = 100 * 1024
)

// Progress performs adaptive progress commits: the first change after an idle
// period commits at once, then each commit delays the next by at least the
// minimum interval and by its written size at 100 KiB/s. At most one commit is
// in flight; changes meanwhile coalesce.
type Progress struct {
	mu       sync.Mutex
	write    func() (int, error)
	onError  func(error)
	waiters  []*ProgressWaiter
	timer    *time.Timer
	inFlight bool
	nextAt   time.Time
	dirty    bool
	stopped  bool

	// minIntervalMs and bytesPerSecond pace commits; tests shorten them.
	minIntervalMs  int
	bytesPerSecond int
}

// ProgressWaiter settles with the commit that includes its change.
type ProgressWaiter struct {
	done chan error
}

// Wait blocks until the commit settles.
func (w *ProgressWaiter) Wait() error { return <-w.done }

// Resolve settles the waiter successfully (the final commit's caller).
func (w *ProgressWaiter) Resolve() { w.done <- nil }

// Reject settles the waiter with an error (the final commit's caller).
func (w *ProgressWaiter) Reject(err error) { w.done <- err }

// NewProgress builds a progress committer; optional minIntervalMs overrides the 100 ms default.
func NewProgress(write func() (int, error), onError func(error), minIntervalMs ...int) *Progress {
	interval := defaultProgressMinIntervalMs
	if len(minIntervalMs) > 0 {
		interval = minIntervalMs[0]
	}
	return &Progress{
		write: write, onError: onError,
		minIntervalMs: interval, bytesPerSecond: defaultProgressBytesPerSecond,
	}
}

// Mark schedules a commit.
func (p *Progress) Mark() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dirty = true
	p.scheduleLocked()
}

// MarkAndWait schedules a commit and returns a waiter for it.
func (p *Progress) MarkAndWait() *ProgressWaiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	waiter := &ProgressWaiter{done: make(chan error, 1)}
	p.waiters = append(p.waiters, waiter)
	p.dirty = true
	p.scheduleLocked()
	return waiter
}

// Stop stops committing and waits for the commit in flight; it returns the
// waiters the final commit must settle.
func (p *Progress) Stop() []*ProgressWaiter {
	p.mu.Lock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	for {
		p.mu.Lock()
		inFlight := p.inFlight
		p.mu.Unlock()
		if !inFlight {
			break
		}
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	waiters := p.waiters
	p.waiters = nil
	return waiters
}

func (p *Progress) scheduleLocked() {
	if p.stopped || p.timer != nil || p.inFlight {
		return
	}
	wait := time.Until(p.nextAt)
	if wait <= 0 {
		p.startLocked()
		return
	}
	p.timer = time.AfterFunc(wait, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.timer = nil
		p.startLocked()
	})
}

// startLocked starts one commit; the caller holds the lock.
func (p *Progress) startLocked() {
	if p.stopped || !p.dirty {
		return
	}
	p.dirty = false
	waiters := p.waiters
	p.waiters = nil
	started := time.Now()
	p.inFlight = true
	go func() {
		bytes, err := p.write()
		p.mu.Lock()
		if err != nil {
			p.nextAt = started.Add(time.Duration(p.minIntervalMs) * time.Millisecond)
			for _, waiter := range waiters {
				waiter.Reject(err)
			}
			onError := p.onError
			p.mu.Unlock()
			if onError != nil {
				onError(err)
			}
		} else {
			delay := time.Duration(p.minIntervalMs) * time.Millisecond
			if byBytes := time.Duration(bytes*1000/p.bytesPerSecond) * time.Millisecond; byBytes > delay {
				delay = byBytes
			}
			p.nextAt = started.Add(delay)
			for _, waiter := range waiters {
				waiter.Resolve()
			}
			p.mu.Unlock()
		}
		p.mu.Lock()
		p.inFlight = false
		dirty := p.dirty
		stopped := p.stopped
		if dirty && !stopped {
			p.scheduleLocked()
		}
		p.mu.Unlock()
	}()
}
