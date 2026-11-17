package tui

import "testing"

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
