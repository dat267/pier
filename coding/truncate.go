// Package coding is a faithful Go port of @mariozechner/pi-coding-agent's
// core (pi/packages/coding-agent): the built-in coding tools, path
// utilities, truncation rules, and (in progress) the session runner.
//
// Ground truth: pi/packages/coding-agent/src/core/tools at the pinned
// upstream commit.
package coding

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
	"strconv"
)

// Port of core/tools/truncate.ts.
//
// Truncation is based on two independent limits — whichever is hit first
// wins. Never returns partial lines (except the bash tail-truncation edge
// case).

const (
	// DefaultMaxLines is the default line limit.
	DefaultMaxLines = 2000
	// DefaultMaxBytes is the default byte limit (50KB).
	DefaultMaxBytes = 50 * 1024
	// GrepMaxLineLength is the max chars per grep match line.
	GrepMaxLineLength = 500
	// uncappedLines is where a tool reads a whole result and limits only by
	// bytes. The value is recorded in the tool's truncation details
	// (`details.truncation = truncation`), so it is observable: upstream passes
	// Number.MAX_SAFE_INTEGER at core/tools/{find,ls,grep}.ts, and that number
	// is what a persisted detail must show.
	uncappedLines = 9007199254740991
)

// TruncatedBy identifies which limit fired.
type TruncatedBy = string

const (
	TruncatedByLines TruncatedBy = "lines"
	TruncatedByBytes TruncatedBy = "bytes"
)

// TruncationResult is the shared truncation outcome.
type TruncationResult struct {
	// Content is the truncated content.
	Content string
	// Truncated reports whether truncation occurred.
	Truncated bool
	// TruncatedBy: "lines", "bytes", or "" when not truncated.
	TruncatedBy TruncatedBy
	// TotalLines/TotalBytes describe the original content.
	TotalLines int
	TotalBytes int
	// OutputLines/OutputBytes describe the truncated output.
	OutputLines int
	OutputBytes int
	// LastLinePartial: only for the tail-truncation edge case.
	LastLinePartial bool
	// FirstLineExceedsLimit: for head truncation.
	FirstLineExceedsLimit bool
	// MaxLines/MaxBytes are the applied limits.
	MaxLines int
	MaxBytes int
}

// MarshalJSON encodes the upstream truncate.ts shape: camelCase keys and
// `truncatedBy: null` when no limit fired.
func (r TruncationResult) MarshalJSON() ([]byte, error) {
	content, err := ai.MarshalJSON(r.Content)
	if err != nil {
		return nil, err
	}
	truncatedBy := []byte("null")
	if r.TruncatedBy != "" {
		truncatedBy, err = ai.MarshalJSON(r.TruncatedBy)
		if err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `{"content":%s,"truncated":%t,"truncatedBy":%s,"totalLines":%d,"totalBytes":%d,"outputLines":%d,"outputBytes":%d,"lastLinePartial":%t,"firstLineExceedsLimit":%t,"maxLines":%d,"maxBytes":%d}`,
		content, r.Truncated, truncatedBy, r.TotalLines, r.TotalBytes, r.OutputLines, r.OutputBytes,
		r.LastLinePartial, r.FirstLineExceedsLimit, r.MaxLines, r.MaxBytes)
	return buf.Bytes(), nil
}

// TruncationOptions carry the limits (zero → defaults).
type TruncationOptions struct {
	MaxLines int
	MaxBytes int
}

func splitLinesForCounting(content string) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// MiddleTruncationResult is the truncate-middle outcome (upstream
// MiddleTruncationResult).
type MiddleTruncationResult struct {
	Content      string
	Truncated    bool
	RemovedChars int
	TotalBytes   int
	TotalLines   int
}

// TruncateMiddle keeps the start and the end of content (half of maxBytes
// each) and replaces the middle with a `…N chars truncated…` marker, like
// Codex does for tool output. Cuts only at character boundaries (upstream
// truncateMiddle).
func TruncateMiddle(content string, maxBytes int) MiddleTruncationResult {
	totalLines := len(splitLinesForCounting(content))
	if len(content) <= maxBytes {
		return MiddleTruncationResult{Content: content, TotalBytes: len(content), TotalLines: totalLines}
	}
	// Continuation bytes (10xxxxxx) are not character starts.
	isBoundary := func(index int) bool {
		return index >= len(content) || content[index]&0xc0 != 0x80
	}
	// Math.floor, not Go's truncating division: for a negative maxBytes the
	// reference's headEnd is negative, which subarray then resolves from the
	// end (A467).
	headEnd := floorDiv2(maxBytes)
	for headEnd > 0 && !isBoundary(headEnd) {
		headEnd--
	}
	// The reference slices a Buffer with these indices, and subarray resolves a
	// negative index from the end and clamps both ends to the buffer, so any
	// maxBytes (including a non-positive or tiny one) produces a defined result.
	// An unclamped Go slice panicked instead: a non-positive maxBytes pushed
	// tailStart below zero or past headEnd (D424).
	tailStart := resolveSubarrayIndex(len(content)-(maxBytes-maxBytes/2), len(content))
	for tailStart < len(content) && !isBoundary(tailStart) {
		tailStart++
	}
	headIndex := resolveSubarrayIndex(headEnd, len(content))
	tailIndex := tailStart
	// subarray(end, start) with start below end yields an empty range.
	middleStart, middleEnd := headIndex, tailIndex
	if tailIndex < headIndex {
		tailIndex = headIndex
		middleEnd = middleStart
	}
	head := content[:headIndex]
	tail := content[tailIndex:]
	removedChars := utf8.RuneCountInString(content[middleStart:middleEnd])
	return MiddleTruncationResult{
		Content:      head + "…" + strconv.Itoa(removedChars) + " chars truncated…" + tail,
		Truncated:    true,
		RemovedChars: removedChars,
		TotalBytes:   len(content),
		TotalLines:   totalLines,
	}
}

// FormatSize formats bytes as a human-readable size.
// floorDiv2 is Math.floor(value / 2).
func floorDiv2(value int) int {
	if value >= 0 {
		return value / 2
	}
	return -((-value + 1) / 2)
}

// resolveSubarrayIndex applies Buffer.prototype.subarray's index rule: a
// negative index counts from the end, and either sign clamps to [0, length].
func resolveSubarrayIndex(index, length int) int {
	if index < 0 {
		index += length
	}
	if index < 0 {
		return 0
	}
	if index > length {
		return length
	}
	return index
}

func FormatSize(bytes int64) string { return FormatSizeNumber(float64(bytes)) }

// FormatSizeNumber renders a byte count the way upstream's formatSize(number)
// does: a JavaScript number can be fractional or exceed the int64 range (an MCP
// server reports resource sizes as `number`), and the decimal rules are those
// of Number.toFixed(1), not Go's
// %.1f.
func FormatSizeNumber(bytes float64) string {
	if bytes < 1024 {
		return ai.JSNumberToString(bytes) + "B"
	} else if bytes < 1024*1024 {
		return ai.JSNumberToFixed(bytes/1024, 1) + "KB"
	}
	return ai.JSNumberToFixed(bytes/(1024*1024), 1) + "MB"
}

// TruncateHead keeps the first N lines/bytes (file reads). Never returns
// partial lines; a first line over the byte limit yields empty content with
// FirstLineExceedsLimit.
func TruncateHead(content string, options TruncationOptions) TruncationResult {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}

	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content: content, Truncated: false,
			TotalLines: totalLines, TotalBytes: totalBytes,
			OutputLines: totalLines, OutputBytes: totalBytes,
			MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}

	if firstLineBytes := len(lines[0]); firstLineBytes > maxBytes {
		return TruncationResult{
			Content: "", Truncated: true, TruncatedBy: TruncatedByBytes,
			TotalLines: totalLines, TotalBytes: totalBytes,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines, MaxBytes: maxBytes,
		}
	}

	var outputLines []string
	outputBytesCount := 0
	truncatedBy := TruncatedByLines

	for i := 0; i < len(lines) && i < maxLines; i++ {
		lineBytes := len(lines[i]) + min(i, 1) // +1 for the newline after the first
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = TruncatedByBytes
			break
		}
		outputLines = append(outputLines, lines[i])
		outputBytesCount += lineBytes
	}

	if len(outputLines) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = TruncatedByLines
	}

	outputContent := strings.Join(outputLines, "\n")
	return TruncationResult{
		Content: outputContent, Truncated: true, TruncatedBy: truncatedBy,
		TotalLines: totalLines, TotalBytes: totalBytes,
		OutputLines: len(outputLines), OutputBytes: len(outputContent),
		MaxLines: maxLines, MaxBytes: maxBytes,
	}
}

// TruncateTail keeps the last N lines/bytes (bash output). May return a
// partial first line when the last original line exceeds the byte limit.
func TruncateTail(content string, options TruncationOptions) TruncationResult {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}

	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content: content, Truncated: false,
			TotalLines: totalLines, TotalBytes: totalBytes,
			OutputLines: totalLines, OutputBytes: totalBytes,
			MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}

	// Collect the window backwards, then reverse it once: prepending into a
	// fresh slice per kept line made this quadratic (34 MB allocated to keep
	// 2000 lines), and the bash tool snapshots its output on every 64 KB read.
	reversed := make([]string, 0, max(0, min(maxLines, totalLines)))
	outputBytesCount := 0
	truncatedBy := TruncatedByLines
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(reversed) < maxLines; i-- {
		lineBytes := len(lines[i])
		if len(reversed) > 0 {
			lineBytes++ // +1 for the newline
		}
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = TruncatedByBytes
			if len(reversed) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(lines[i], maxBytes)
				reversed = append(reversed, truncatedLine)
				outputBytesCount = len(truncatedLine)
				lastLinePartial = true
			}
			break
		}
		reversed = append(reversed, lines[i])
		outputBytesCount += lineBytes
	}

	outputLines := make([]string, len(reversed))
	for i, line := range reversed {
		outputLines[len(reversed)-1-i] = line
	}

	if len(outputLines) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = TruncatedByLines
	}

	outputContent := strings.Join(outputLines, "\n")
	return TruncationResult{
		Content: outputContent, Truncated: true, TruncatedBy: truncatedBy,
		TotalLines: totalLines, TotalBytes: totalBytes,
		OutputLines: len(outputLines), OutputBytes: len(outputContent),
		LastLinePartial: lastLinePartial,
		MaxLines:        maxLines, MaxBytes: maxBytes,
	}
}

// truncateStringToBytesFromEnd truncates to a byte limit from the end,
// respecting UTF-8 boundaries.
func truncateStringToBytesFromEnd(str string, maxBytes int) string {
	if len(str) <= maxBytes {
		return str
	}
	// Buffer.prototype.slice clamps both ends, so a start past the end (which a
	// negative maxBytes produces) yields an empty string instead of an
	// out-of-range Go slice (A468).
	start := resolveSubarrayIndex(len(str)-maxBytes, len(str))
	// Find a valid UTF-8 boundary.
	for start < len(str) && (str[start]&0xc0) == 0x80 {
		start++
	}
	return str[start:]
}

// TruncateLineOutput is the truncateLine result.
type TruncateLineOutput struct {
	Text         string
	WasTruncated bool
}

// TruncateLine truncates a single line to maxChars with a [truncated] suffix
// (grep match lines). Upstream's default applies to an omitted argument; Go
// callers always pass the limit, so an explicit 0 truncates everything.
func TruncateLine(line string, maxChars int) TruncateLineOutput {
	// The prefix scan answers `line.length <= maxChars` too: the whole line
	// fits exactly when the longest whole-character prefix is the line (D517).
	head, whole := sliceHeadPrefix(line, maxChars)
	if whole {
		return TruncateLineOutput{Text: line, WasTruncated: false}
	}
	return TruncateLineOutput{
		Text:         head + "... [truncated]",
		WasTruncated: true,
	}
}

// SliceHead returns at most the first maxUnits UTF-16 code units of text without
// splitting a surrogate pair (pi v1.5.2 truncate.ts sliceHead): a lone high
// surrogate before the cut would be invalid UTF-16, so the whole character is
// left out. A negative budget yields "", where JavaScript's `slice(0, -1)`
// dropped the last character instead.
func SliceHead(text string, maxUnits int) string {
	head, _ := sliceHeadPrefix(text, maxUnits)
	return head
}

// sliceHeadPrefix returns SliceHead(text, maxUnits) and whether it is all of
// text, scanning only the bytes it keeps (D517). The result shares text's
// memory; both callers append a suffix, which copies it. Invalid UTF-8 in the
// kept prefix is replaced byte by byte with U+FFFD, as the UTF-16 round-trip
// (`utf16.Encode([]rune(text))`) this replaces did.
func sliceHeadPrefix(text string, maxUnits int) (string, bool) {
	if maxUnits <= 0 {
		// A zero budget keeps only an empty text whole; a negative one never
		// fits, as `"".length <= -1` is false upstream.
		return "", maxUnits == 0 && text == ""
	}
	limit := min(len(text), maxUnits)
	start := asciiPrefixLength(text[:limit])
	if start == limit {
		// ASCII bytes are whole one-unit characters, so the cut is exact.
		return text[:limit], limit == len(text)
	}
	units, invalid := start, false
	for offset, r := range text[start:] {
		width := 1
		if r >= 0x10000 {
			width = 2
		}
		if units+width > maxUnits {
			return replaceInvalidUTF8(text[:start+offset], invalid), false
		}
		if r == utf8.RuneError && !strings.HasPrefix(text[start+offset:], "�") {
			invalid = true
		}
		units += width
	}
	return replaceInvalidUTF8(text, invalid), true
}

// asciiPrefixLength returns the length of the leading run of ASCII bytes,
// testing eight bytes per step. The shifts compile to one 64-bit load, which
// measured twice as fast as OR-ing the bytes (D517).
func asciiPrefixLength(s string) int {
	index := 0
	for ; index+8 <= len(s); index += 8 {
		chunk := s[index : index+8]
		word := uint64(chunk[0]) | uint64(chunk[1])<<8 | uint64(chunk[2])<<16 | uint64(chunk[3])<<24 |
			uint64(chunk[4])<<32 | uint64(chunk[5])<<40 | uint64(chunk[6])<<48 | uint64(chunk[7])<<56
		if word&0x8080808080808080 != 0 {
			break
		}
	}
	for index < len(s) && s[index] < utf8.RuneSelf {
		index++
	}
	return index
}

// replaceInvalidUTF8 maps each invalid byte to U+FFFD, the decoding
// []rune(s) applies, when the scan found one.
func replaceInvalidUTF8(s string, invalid bool) string {
	if !invalid {
		return s
	}
	return string([]rune(s))
}
