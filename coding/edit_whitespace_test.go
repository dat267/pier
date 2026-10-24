package coding

import "testing"

// Reference: pi v1.4.0 core/tools/edit-diff.ts countOccurrences uses JS split;
// splitting on an empty normalized target counts UTF-16 units minus one.
func TestEmptyNormalizedOccurrenceCountMatchesSplit(t *testing.T) {
	if got := countOccurrences(" ", " "); got != -1 {
		t.Fatalf("count=%d; want -1 from ''.split('').length - 1", got)
	}
}

func TestEditUniqueWhitespaceTarget(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"a ", "aX"}, {" a", "Xa"}, {"ab ", "abX"}} {
		got, err := ApplyEditsToNormalizedContent(tc.input, []Edit{{OldText: " ", NewText: "X"}}, "f.txt")
		if err != nil {
			t.Errorf("%q: %v", tc.input, err)
			continue
		}
		if got.NewContent != tc.want {
			t.Errorf("%q: %+v; want %q", tc.input, got, tc.want)
		}
	}
}
