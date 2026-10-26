package ai

import (
	"regexp"
	"testing"
)

// JSWhitespaceClass is built from the ECMAScript `\s` set; this pins its
// boundaries with the near misses that a truncated range would break. The set
// was verified once against every Unicode code point executed by the pinned
// engine (25 units: 0x9-0xd, 0x20, 0xa0, 0x1680, 0x2000-0x200a, 0x2028,
// 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff).
func TestJSWhitespaceClassBoundaries(t *testing.T) {
	class := regexp.MustCompile(`\A[` + JSWhitespaceClass + `]\z`)
	for _, unit := range []struct {
		code  rune
		space bool
	}{
		{0x08, false}, {0x09, true}, {0x0a, true}, {0x0b, true}, {0x0c, true}, {0x0d, true}, {0x0e, false},
		{0x20, true}, {0x1f, false}, {0x21, false}, {0x9f, false}, {0xa0, true}, {0xa1, false},
		{0x167f, false}, {0x1680, true}, {0x1681, false},
		{0x1fff, false}, {0x2000, true}, {0x200a, true}, {0x200b, false},
		{0x2027, false}, {0x2028, true}, {0x2029, true}, {0x202a, false},
		{0x202e, false}, {0x202f, true}, {0x2030, false},
		{0x205e, false}, {0x205f, true}, {0x2060, false},
		{0x2fff, false}, {0x3000, true}, {0x3001, false},
		{0xfefe, false}, {0xfeff, true}, {0xff00, false},
	} {
		if got := class.MatchString(string(unit.code)); got != unit.space {
			t.Errorf("U+%04X: in JavaScript \\s = %v, want %v", unit.code, got, unit.space)
		}
	}
	// The class is also what JSTrim trims, so the two must agree on those units.
	for _, code := range []rune{0x20, 0xa0, 0x1680, 0x2007, 0x2028, 0x202f, 0x205f, 0x3000, 0xfeff} {
		if !IsJSSpace(code) {
			t.Errorf("U+%04X: IsJSSpace = false, want true", code)
		}
	}
}
