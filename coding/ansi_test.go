package coding

import (
	"encoding/json"
	"math/rand"
	"os"
	"strings"
	"testing"
)

func referenceStripAnsi(value string) string {
	return ansiStripPattern.ReplaceAllString(value, "")
}

// TestStripAnsiMatchesPattern pins the scanner to the reference pattern over a
// corpus of hand-picked sequences plus fuzzed strings.
func TestStripAnsiMatchesPattern(t *testing.T) {
	cases := []string{
		"",
		"plain text",
		"\x1b[0m",
		"\x1b[1;31mred\x1b[0m",
		"\x1b[38;2;255;0;0mtruecolor\x1b[39m",
		"\x1b[38:2:255:0:0mcolon params\x1b[0m",
		"\x1b[K",
		"\x1b[2K",
		"\x1b[?25l",
		"\x1b[?25h",
		"\x1b(B",
		"\x1b[>0c",
		"\x1b[#8",
		"\x1b]0;window title\x07",
		"\x1b]0;window title\x1b\\",
		"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\",
		"\x1b]0;unterminated title",
		"\x1b]",
		"\x1b",
		"\x1b[",
		"\x1b[;",
		"\x1bz",
		"\x1b[12345;6789m",
		"\x1b[;5m",
		"a\x1b[0mb\x1b[1mc",
		"\x1b[0m\x1b[0m",
		// The final-byte class is A-P, R-T, Z: S (scroll up) is stripped and
		// U-Y are not (pi v1.5.0 utils/ansi.ts CSI_FINAL).
		"\x1b[2Sx\x1b[3Tx\x1b[4Ux\x1b[5Vx\x1b[6Wx\x1b[7Xx\x1b[8Yx\x1b[9Zx",
		"\u009bS",
		"\x1b]0;t\x07dangling\x1b[",
		"\u009b0m",
		"\u009b31mred\u009b0m",
		"pre \u009b?25l post",
		"\x1b]title\u009c",
		"text with \x1b2K lines",
	}
	for _, value := range cases {
		if got, want := StripAnsi(value), referenceStripAnsi(value); got != want {
			t.Errorf("StripAnsi(%q) = %q, want %q", value, got, want)
		}
	}

	// Fuzz: random sequences over the escape grammar alphabet.
	alphabet := []string{
		"\x1b", "\u009b", "\u009c", "\x07", "[", "]", "(", ")", "#", ";", ":", "?", "\\",
		"0", "1", "9", "25", "1234", "12345", "m", "K", "H", "l", "h", "B", "c", "f", "n", "q", "u", "y", "=", ">", "<", "~",
		"A", "P", "R", "T", "Z", "a", "g", "o", "r", "s", "t", "z", " ", "x", "\x1b]0;t", "\x1b\\",
	}
	rng := rand.New(rand.NewSource(1))
	for i := range 30000 {
		var sb strings.Builder
		parts := rng.Intn(8)
		for range parts {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		value := sb.String()
		if got, want := StripAnsi(value), referenceStripAnsi(value); got != want {
			t.Fatalf("fuzz %d: StripAnsi(%q) = %q, want %q", i, value, got, want)
		}
	}
}

func BenchmarkStripAnsiColoured(b *testing.B) {
	var sb strings.Builder
	for range 1000 {
		sb.WriteString("line: \x1b[32mok\x1b[0m and \x1b[1;31mfail\x1b[0m and \x1b]0;t\x07 text\n")
	}
	value := sb.String()
	b.SetBytes(int64(len(value)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = StripAnsi(value)
	}
}

func BenchmarkStripAnsiPatternReference(b *testing.B) {
	var sb strings.Builder
	for range 1000 {
		sb.WriteString("line: \x1b[32mok\x1b[0m and \x1b[1;31mfail\x1b[0m and \x1b]0;t\x07 text\n")
	}
	value := sb.String()
	b.SetBytes(int64(len(value)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = referenceStripAnsi(value)
	}
}

// Reference: pi f9a26365 utils/ansi.ts CSI_START accepts a private prefix
// (<, =, >) only immediately after a real CSI introducer, so SGR mouse
// reports and private DA replies are stripped whole while a bare ESC keeps
// its final-byte meaning.
func TestStripAnsiPrivatePrefixCSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[<35;59;1M":          "",
		"\x1b[<0;10;5M":           "",
		"\x1b[<35;59;1Mtext":      "text",
		"\x1b[=1;2c":              "",
		"\x1b[>0c":                "",
		"\x1b[>1;10;0c":           "",
		"\u009b<35;59;1M":         "",
		"\u009b>0c":               "",
		"before\x1b[<1;2;3Mafter": "beforeafter",
		// A bare ESC keeps < = > as final bytes (keypad modes).
		"\x1b<":   "",
		"\x1b=":   "",
		"\x1b>":   "",
		"a\x1b<b": "ab",
	}
	for value, want := range cases {
		if got := StripAnsi(value); got != want {
			t.Errorf("StripAnsi(%q) = %q, want %q", value, got, want)
		}
	}
}

// A private-prefix CSI split across chunks is held back whole, so its
// parameters cannot leak as text while streaming.
func TestSplitIncompleteAnsiSuffixPrivatePrefixCSI(t *testing.T) {
	complete, pending := splitIncompleteAnsiSuffix("abc\x1b[<35;59")
	if complete != "abc" || pending != "\x1b[<35;59" {
		t.Fatalf("split = %q / %q", complete, pending)
	}
	complete, pending = splitIncompleteAnsiSuffix("abc\x1b[<35;59;1M")
	if complete != "abc\x1b[<35;59;1M" || pending != "" {
		t.Fatalf("complete split = %q / %q", complete, pending)
	}
	complete, pending = splitIncompleteAnsiSuffix("abc\u009b>0c")
	if complete != "abc\u009b>0c" || pending != "" {
		t.Fatalf("C1 complete split = %q / %q", complete, pending)
	}
}

// Reference: pi v1.5.0 (f9a26365), utils/ansi.ts (`stripAnsi`,
// `splitIncompleteAnsiSuffix`). The corpus is that of test/ansi-utils.test.ts
// `getCompatibilityInputs` (an escape-flavoured arbitrary corpus, including
// unterminated OSC, DCS/SOS/PM/APC strings, 8-bit CSI and single-byte ESC
// sequences) plus the cases that test asserts directly for issue 11377:
// private-parameter CSI (`ESC [ < … M`, which real terminals send for a mouse
// press) and bare keypad-mode ESC sequences, where the digits that follow must
// stay.
//
// Regenerate: node --experimental-strip-types testdata/ansi-compat/generate.mjs ../pi > testdata/ansi-compat/golden.json
func TestAnsiCompatMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/ansi-compat/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input    string `json:"input"`
		Strip    string `json:"strip"`
		Complete string `json:"complete"`
		Pending  string `json:"pending"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 200 {
		t.Fatalf("fixture has %d cases, expected the full corpus", len(cases))
	}
	for _, testCase := range cases {
		if got := StripAnsi(testCase.Input); got != testCase.Strip {
			t.Errorf("StripAnsi(%q) = %q, want %q", testCase.Input, got, testCase.Strip)
		}
		complete, pending := splitIncompleteAnsiSuffix(testCase.Input)
		if complete != testCase.Complete || pending != testCase.Pending {
			t.Errorf("splitIncompleteAnsiSuffix(%q) = %q/%q, want %q/%q",
				testCase.Input, complete, pending, testCase.Complete, testCase.Pending)
		}
	}
}
