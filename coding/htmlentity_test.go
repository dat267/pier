package coding

import "testing"

// Reference: pi v1.3.0 utils/html.ts decodes a numeric entity with
// Number.parseInt and String.fromCodePoint. parseInt skips leading whitespace,
// accepts a sign and a 0x prefix in base 16, and ignores the trailing text after
// the last valid digit; the port used strconv.ParseInt, which requires every
// character to be a digit. Each expectation was read off the reference.
func TestDecodeHtmlEntityMatchesParseInt(t *testing.T) {
	cases := []struct {
		entity string
		want   string
		ok     bool
	}{
		{"amp", "&", true},
		{"lt", "<", true},
		{"gt", ">", true},
		{"quot", "\"", true},
		{"apos", "'", true},
		{"#65", "A", true},
		{"#x41", "A", true},
		{"#X41", "A", true},
		{"# 65", "A", true},
		{"#65 ", "A", true},
		{"#x 41", "A", true},
		{"#\t65", "A", true},
		{"#  65", "A", true},
		{"#12abc", "\f", true},
		{"#x1f600zz", "\U0001F600", true},
		{"#x0x41", "A", true},
		{"#x0X41", "A", true},
		{"#0x41", "\x00", true},
		{"#+65", "A", true},
		{"#-0", "\x00", true},
		{"#- 1", "", false},
		{"#-1", "", false},
		{"#", "", false},
		{"#x", "", false},
		{"#x0x", "", false},
		{"#xffffffff", "", false},
		{"#1114111", "\U0010FFFF", true},
		{"#1114112", "", false},
		{"unknown", "", false},
	}
	for _, testCase := range cases {
		got, ok := DecodeHtmlEntity(testCase.entity)
		if got != testCase.want || ok != testCase.ok {
			t.Errorf("DecodeHtmlEntity(%q) = %q, %v; want %q, %v", testCase.entity, got, ok, testCase.want, testCase.ok)
		}
	}
}

// TestDecodeHtmlEntityAtBoundary covers the reference's 16-character window:
// the semicolon may sit at most sixteen code units after the "&".
func TestDecodeHtmlEntityAtBoundary(t *testing.T) {
	cases := []struct {
		html   string
		want   string
		length int
		ok     bool
	}{
		{"&#65;", "A", 5, true},
		{"&#000000000065;", "A", 15, true},
		{"&#0000000000065;", "A", 16, true},
		{"&#00000000000065;", "A", 17, true},
		{"&#000000000000065;", "", 0, false},
		{"&#0000000000000065;", "", 0, false},
		{"&# 65;", "A", 6, true},
		{"&#12abc;", "\f", 8, true},
		{"&unknown;", "", 0, false},
		{"&nosemicolon", "", 0, false},
	}
	for _, testCase := range cases {
		decoded, ok := DecodeHtmlEntityAt(testCase.html, 0)
		if decoded.Text != testCase.want || ok != testCase.ok || (ok && decoded.Length != testCase.length) {
			t.Errorf("DecodeHtmlEntityAt(%q) = %+v, %v; want %q, %d, %v",
				testCase.html, decoded, ok, testCase.want, testCase.length, testCase.ok)
		}
	}
}
