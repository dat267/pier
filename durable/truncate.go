package durable

import (
	"fmt"
	"strings"
)

// Port of truncate.ts: bounded tool-output truncation. Truncation is based on
// two independent limits, whichever is hit first, and never returns partial
// lines.

// Default truncation limits (upstream DEFAULT_MAX_LINES / DEFAULT_MAX_BYTES).
const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024
)

// TruncationResult is the outcome of one truncation (upstream
// TruncationResult).
type TruncationResult struct {
	// Content is the truncated content.
	Content string
	// Truncated reports whether truncation occurred.
	Truncated bool
	// TruncatedBy is "lines", "bytes", or "" when not truncated.
	TruncatedBy string
	// TotalLines and TotalBytes describe the original content.
	TotalLines int
	TotalBytes int
	// OutputLines and OutputBytes describe the truncated output.
	OutputLines int
	OutputBytes int
	// LastLinePartial is set when the last line was partially truncated (tail
	// truncation edge case).
	LastLinePartial bool
	// FirstLineExceedsLimit is set when the first line alone exceeds the byte
	// limit (head truncation).
	FirstLineExceedsLimit bool
	// MaxLines and MaxBytes are the limits that were applied.
	MaxLines int
	MaxBytes int
}

// TruncationOptions bounds one truncation (upstream TruncationOptions).
type TruncationOptions struct {
	MaxLines int
	MaxBytes int
}

// UTF8ByteLength is the UTF-8 byte length of a string.
func UTF8ByteLength(content string) int { return len(content) }

// FormatSize renders a byte count as a human-readable size.
func FormatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

// splitLinesForCounting splits content into lines; a trailing newline does not
// add an empty final line.
func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// TruncateHead keeps the first maxLines lines / maxBytes bytes.
func TruncateHead(content string, options TruncationOptions) TruncationResult {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	totalBytes := UTF8ByteLength(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content: content, Truncated: false, TruncatedBy: "",
			TotalLines: totalLines, TotalBytes: totalBytes,
			OutputLines: totalLines, OutputBytes: totalBytes,
			MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}
	firstLineBytes := 0
	if len(lines) > 0 {
		firstLineBytes = UTF8ByteLength(lines[0])
	}
	if firstLineBytes > maxBytes {
		return TruncationResult{
			Content: "", Truncated: true, TruncatedBy: "bytes",
			TotalLines: totalLines, TotalBytes: totalBytes,
			FirstLineExceedsLimit: true, MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}
	var outputLines []string
	outputBytes := 0
	truncatedBy := "lines"
	for index := 0; index < len(lines) && index < maxLines; index++ {
		line := lines[index]
		lineBytes := UTF8ByteLength(line)
		if index > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		outputLines = append(outputLines, line)
		outputBytes += lineBytes
	}
	if truncatedBy != "bytes" {
		if len(outputLines) < totalLines {
			truncatedBy = "lines"
		} else {
			truncatedBy = "bytes"
		}
	}
	outputContent := strings.Join(outputLines, "\n")
	return TruncationResult{
		Content: outputContent, Truncated: true, TruncatedBy: truncatedBy,
		TotalLines: totalLines, TotalBytes: totalBytes,
		OutputLines: len(outputLines), OutputBytes: UTF8ByteLength(outputContent),
		MaxLines: maxLines, MaxBytes: maxBytes,
	}
}
