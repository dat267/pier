package coding

import (
	context "context"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) core/bash-executor.ts tracks `totalBytes` as
// the raw byte length of each chunk (`totalBytes += data.length` in onData) and
// starts the full-output file once it exceeds DEFAULT_MAX_BYTES, before any
// sanitizing: `if (totalBytes > DEFAULT_MAX_BYTES) ensureTempFile()`. A stream
// that is almost entirely ANSI escapes therefore still produces
// `fullOutputPath`, even though its sanitized text is tiny and truncateTail
// reports no truncation.
func TestBashExecutorFullOutputFileUsesRawBytes(t *testing.T) {
	esc := "\x1b"
	zero := 0
	run := func(t *testing.T, chunks []string) BashResult {
		t.Helper()
		result, err := ExecuteBashWithOperations(context.Background(), "cmd", "/tmp",
			&scriptedOps{chunks: chunks, exitCode: &zero}, &BashExecutorOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	// Escape codes only: ~76 KB raw, 2 bytes of text.
	var ansiHeavy string
	for range 9000 {
		ansiHeavy += esc + "[31m" + esc + "[0m"
	}
	ansiHeavy += "hi"
	result := run(t, []string{ansiHeavy})
	if result.FullOutputPath == "" {
		t.Errorf("ANSI-heavy stream has no full-output file; output %q, truncated %v", result.Output, result.Truncated)
	}
	if result.Truncated || result.Output != "hi" {
		t.Errorf("output %q truncated %v, want %q and false", result.Output, result.Truncated, "hi")
	}

	// Controls: the threshold is the raw byte count, strictly greater than
	// DEFAULT_MAX_BYTES, and plain oversized text is still truncated.
	if got := run(t, []string{jsRepeatString("z", DefaultMaxBytes)}); got.FullOutputPath != "" || got.Truncated {
		t.Errorf("exactly DefaultMaxBytes: file %q truncated %v, want none", got.FullOutputPath, got.Truncated)
	}
	if got := run(t, []string{jsRepeatString("z", DefaultMaxBytes+1)}); got.FullOutputPath == "" || !got.Truncated {
		t.Errorf("DefaultMaxBytes+1: file %q truncated %v, want file and truncated", got.FullOutputPath, got.Truncated)
	}
}

func jsRepeatString(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
