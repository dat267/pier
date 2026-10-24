package coding

import "testing"

// Reference: pi v1.4.0 core/tools/truncate.ts, `truncateLine(line, maxChars)`:
// the default applies to an omitted argument only, so an explicit 0 truncates
// everything and keeps the suffix. The port treated 0 as "use the default".
func TestTruncateLineZeroLimit(t *testing.T) {
	result := TruncateLine("abcdef", 0)
	if !result.WasTruncated || result.Text != "... [truncated]" {
		t.Fatalf("TruncateLine(abcdef, 0) = %+v", result)
	}
	if got := TruncateLine("abc", 3); got.WasTruncated || got.Text != "abc" {
		t.Fatalf("TruncateLine(abc, 3) = %+v", got)
	}
}
