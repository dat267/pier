package coding

import "testing"

// splitLinesWithEndings mirrors JS `content.match(/[^\n]*\n|[^\n]+/g)`: a
// trailing newline terminates the last line instead of producing an extra empty
// one.
func TestSplitLinesWithEndings(t *testing.T) {
	for _, testCase := range []struct {
		content string
		want    []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\n", []string{"a\n"}},
		{"a\nb", []string{"a\n", "b"}},
		{"a\n\nb", []string{"a\n", "\n", "b"}},
		{"\n", []string{"\n"}},
		{"a\r\nb", []string{"a\r\n", "b"}},
	} {
		got := splitLinesWithEndings(testCase.content)
		if len(got) != len(testCase.want) {
			t.Fatalf("%q: %q, want %q", testCase.content, got, testCase.want)
		}
		for index := range got {
			if got[index] != testCase.want[index] {
				t.Fatalf("%q: %q, want %q", testCase.content, got, testCase.want)
			}
		}
	}
}
