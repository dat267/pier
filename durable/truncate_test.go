package durable

import (
	"strings"
	"testing"
)

// Port of truncate.ts.

func TestUTF8ByteLength(t *testing.T) {
	if got := UTF8ByteLength("abc"); got != 3 {
		t.Fatalf("ascii = %d", got)
	}
	if got := UTF8ByteLength("aé"); got != 3 {
		t.Fatalf("two-byte = %d", got)
	}
	if got := UTF8ByteLength("😀"); got != 4 {
		t.Fatalf("four-byte = %d", got)
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int]string{
		500:             "500B",
		1024:            "1.0KB",
		1536:            "1.5KB",
		2 * 1024 * 1024: "2.0MB",
	}
	for bytes, want := range cases {
		if got := FormatSize(bytes); got != want {
			t.Fatalf("FormatSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}

func TestTruncateHeadNoTruncation(t *testing.T) {
	result := TruncateHead("a\nb", TruncationOptions{})
	if result.Truncated || result.TruncatedBy != "" || result.Content != "a\nb" ||
		result.TotalLines != 2 || result.OutputLines != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestTruncateHeadLineLimit(t *testing.T) {
	result := TruncateHead("l1\nl2\nl3\nl4\nl5", TruncationOptions{MaxLines: 2})
	if !result.Truncated || result.TruncatedBy != "lines" || result.Content != "l1\nl2" {
		t.Fatalf("result = %+v", result)
	}
	if result.TotalLines != 5 || result.OutputLines != 2 {
		t.Fatalf("counts = %+v", result)
	}
}

func TestTruncateHeadByteLimit(t *testing.T) {
	result := TruncateHead("aaaa\nbbbb", TruncationOptions{MaxBytes: 6})
	if !result.Truncated || result.TruncatedBy != "bytes" || result.Content != "aaaa" {
		t.Fatalf("result = %+v", result)
	}
	// The first line alone exceeds the byte limit.
	result = TruncateHead("aaaaaaaa", TruncationOptions{MaxBytes: 4})
	if result.Content != "" || !result.FirstLineExceedsLimit || result.TruncatedBy != "bytes" {
		t.Fatalf("first line = %+v", result)
	}
}

func TestTruncateHeadTrailingNewlineBytes(t *testing.T) {
	// The content is one complete line plus a trailing newline; the byte limit
	// is only exceeded by that newline, so the limit is bytes, not lines.
	result := TruncateHead("aaa\n", TruncationOptions{MaxBytes: 3})
	if result.Content != "aaa" || result.TruncatedBy != "bytes" || result.OutputBytes != 3 {
		t.Fatalf("result = %+v", result)
	}
}

func TestTruncateHeadNeverPartialLines(t *testing.T) {
	var builder strings.Builder
	for index := 0; index < 5; index++ {
		builder.WriteString("line\n")
	}
	result := TruncateHead(builder.String(), TruncationOptions{MaxLines: 3})
	if result.OutputLines != 3 || result.Content != "line\nline\nline" {
		t.Fatalf("result = %+v", result)
	}
	if result.LastLinePartial {
		t.Fatalf("partial line = %+v", result)
	}
}
