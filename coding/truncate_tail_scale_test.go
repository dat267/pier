package coding

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 core/tools/truncate.ts grows its output array only for
// retained lines. An effectively unlimited line budget must not be used as a
// slice capacity when only the byte limit triggers truncation.
func TestTruncateTailSupportsUnboundedLineLimit(t *testing.T) {
	result := TruncateTail("hello", TruncationOptions{MaxLines: math.MaxInt, MaxBytes: 2})
	if result.Content != "lo" || !result.Truncated || result.TruncatedBy != TruncatedByBytes || !result.LastLinePartial {
		t.Fatalf("byte-only truncation = %+v, want partial tail lo", result)
	}
	if result.MaxLines != math.MaxInt {
		t.Fatalf("reported line limit = %d, want %d", result.MaxLines, math.MaxInt)
	}
}

// Reference: pi v1.4.0 core/tools/truncate.ts formatSize divides JavaScript
// numbers before toFixed(1), without an overflowing integer multiplication.
func TestFormatSizeLargeByteCountDoesNotOverflow(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  string
	}{
		{math.MaxInt64, "8796093022208.0MB"},
		{math.MaxInt64 / 2, "4398046511104.0MB"},
	} {
		if got := FormatSize(tc.bytes); got != tc.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

// TestTruncateTailBuildsTheWindowWithoutPrepending pins the bash tool's
// per-chunk snapshot cost: the tail window was built by prepending each kept
// line into a fresh slice, so a 100 KB tail (up to 2000 lines) cost 2713
// allocations and ~7 ms on every output chunk — the tool snapshots per 64 KB
// read, which is why a running command stuttered the UI.
func TestTruncateTailBuildsTheWindowWithoutPrepending(t *testing.T) {
	var sb strings.Builder
	for i := range 4000 {
		fmt.Fprintf(&sb, "build output line %d with some text to make it a realistic width\n", i)
	}
	content := sb.String()

	measure := func(maxLines int) int64 {
		return allocatedBytesPerRun(t, 10, func() {
			_ = TruncateTail(content, TruncationOptions{MaxLines: maxLines, MaxBytes: len(content)})
		})
	}
	smallBytes := measure(50)
	largeBytes := measure(2000)

	// Keeping 40x more lines must not cost ~40x more bytes: the content is
	// split once either way, and the window is appended, not prepended.
	if largeBytes > smallBytes*10 {
		t.Fatalf("truncating to 2000 lines allocated %d bytes vs %d for 50 lines; "+
			"the window is prepended per line (quadratic)", largeBytes, smallBytes)
	}
}

// TestTruncateTailKeepsTheNewestLines pins the window order after the fix.
func TestTruncateTailKeepsTheNewestLines(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive"
	result := TruncateTail(content, TruncationOptions{MaxLines: 2, MaxBytes: len(content)})
	if result.Content != "four\nfive" {
		t.Fatalf("content = %q", result.Content)
	}
	if !result.Truncated || result.TruncatedBy != TruncatedByLines {
		t.Fatalf("truncation = %+v", result)
	}
	if result.OutputLines != 2 || result.TotalLines != 5 {
		t.Fatalf("lines = %d/%d", result.OutputLines, result.TotalLines)
	}
}
