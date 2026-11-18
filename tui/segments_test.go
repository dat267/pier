package tui

import (
	"strings"
	"testing"
)

// TestIndexOfFindsTheFirstByteOccurrence pins the search semantics indexOf has
// to keep (it is the port's String.prototype.indexOf for byte-oriented
// callers): the first occurrence's byte offset, -1 when absent, 0 for an empty
// needle. A hand-rolled O(n*m) loop used to implement it, which cost 20% of a
// warm frame through IsImageLine.
func TestIndexOfFindsTheFirstByteOccurrence(t *testing.T) {
	cases := []struct {
		haystack string
		needle   string
		want     int
	}{
		{"hello world", "world", 6},
		{"hello world", "hello", 0},
		{"hello world", "zz", -1},
		{"hello", "", 0},
		{"hi", "hello", -1},
		{"", "", 0},
		{"abcabc", "bc", 1},
		// Byte offsets, not rune offsets (what the previous byte loop did too).
		{"héllo\x1b_Gx", "\x1b_G", len("héllo")},
		{"日本語", "本", len("日")},
	}
	for _, tc := range cases {
		got := indexOf(tc.haystack, tc.needle)
		if got != tc.want {
			t.Fatalf("indexOf(%q, %q) = %d, want %d", tc.haystack, tc.needle, got, tc.want)
		}
		if got != strings.Index(tc.haystack, tc.needle) {
			t.Fatalf("indexOf(%q, %q) = %d, strings.Index gives %d", tc.haystack, tc.needle, got, strings.Index(tc.haystack, tc.needle))
		}
	}
}

// SliceWithWidth keeps the range [startCol, startCol+length): an escape that
// applies at the end column is outside it, and a wide character that would
// extend past the range is dropped in strict mode.
func TestSliceWithWidthExcludesTheEndColumn(t *testing.T) {
	sliced := SliceWithWidth("ab\x1b[31mcd", 0, 2, false)
	if sliced.Text != "ab" {
		t.Fatalf("slice = %q, want the escape at the end column dropped", sliced.Text)
	}
	if sliced.Width != 2 {
		t.Fatalf("width = %d, want 2", sliced.Width)
	}
	wide := SliceWithWidth("a界", 1, 1, true)
	if wide.Text != "" || wide.Width != 0 {
		t.Fatalf("strict slice of a wide character = %q (%d columns), want empty", wide.Text, wide.Width)
	}
	if plain := SliceWithWidth("abcdef", 0, 2, false); plain.Text != "ab" || plain.Width != 2 {
		t.Fatalf("plain slice = %q (%d columns), want two columns \"ab\"", plain.Text, plain.Width)
	}
	whole := SliceWithWidth("a界b", 0, 3, false)
	if whole.Text != "a界" || whole.Width != 3 {
		t.Fatalf("wide slice = %q (%d columns), want the wide character included", whole.Text, whole.Width)
	}
}
