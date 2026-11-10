package ai

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// referenceJSSlice is the straightforward UTF-16 encoding of the whole string,
// the implementation JSSlice shipped before the prefix fast path. The fast path
// must agree with it on every input (pi v1.5.0 utils/text.ts:48 ownedSlice,
// where V8's slice is O(1) in the returned length).
func referenceJSSlice(s string, start, end int) string {
	units := utf16.Encode([]rune(s))
	length := len(units)
	if start < 0 {
		start = max(length+start, 0)
	} else if start > length {
		start = length
	}
	if end < 0 {
		end = max(length+end, 0)
	} else if end > length {
		end = length
	}
	if start >= end {
		return ""
	}
	return string(utf16.Decode(units[start:end]))
}

// TestJSSliceMatchesFullEncoding pins the prefix fast path to the full UTF-16
// encode for every index combination the callers use.
func TestJSSliceMatchesFullEncoding(t *testing.T) {
	inputs := []string{
		"", "a", "abc", "héllo", "日本語", "a😀b", "😀", "e\u0301x",
		strings.Repeat("a", 300), strings.Repeat("日", 100), strings.Repeat("😀", 60),
		"\xff\xfe", "a\xed\xa0\x80b", "𝕳𝖊𝖑𝖑𝖔",
	}
	indices := []int{-5, -2, -1, 0, 1, 2, 3, 5, 50, 99, 100, 1000, 1 << 30}
	for _, in := range inputs {
		for _, start := range indices {
			for _, end := range indices {
				want := referenceJSSlice(in, start, end)
				got := JSSlice(in, start, end)
				if got != want {
					t.Fatalf("JSSlice(%q, %d, %d) = %q, want %q", in, start, end, got, want)
				}
			}
		}
	}
}

// TestJSSlicePrefixDoesNotScanTheWholeString guards the fix: a short prefix of a
// large body must not allocate in proportion to the body.
func TestJSSlicePrefixDoesNotScanTheWholeString(t *testing.T) {
	body := strings.Repeat("abcdefghij", 800000) // 8 MB
	const prefix = 500
	before := testing.AllocsPerRun(20, func() { _ = referenceJSSlice(body, 0, prefix) })
	allocs := testing.AllocsPerRun(20, func() {
		if got := JSSlice(body, 0, prefix); len(got) != prefix {
			t.Fatalf("len = %d, want %d", len(got), prefix)
		}
	})
	// The prefix must not allocate per byte; it may not even match the full
	// encoding, which allocates a 16 MB uint16 slice for the whole body.
	if allocs > before {
		t.Fatalf("JSSlice allocated %v times for a %d-char prefix of an 8 MB body, want <= %v", allocs, prefix, before)
	}
}
