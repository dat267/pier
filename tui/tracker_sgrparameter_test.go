package tui

import "testing"

// The tracker's SGR field reader clamps a field wider than an int at
// maxSGRParameter instead of wrapping. The reference reads the field with
// Number.parseInt, which yields a huge Number that matches no tracked code, so
// a clamp keeps the same state; a wrap can land on 0, the reset code (A435).
func TestParseSGRNumberClampsWideFields(t *testing.T) {
	for _, test := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"", 0, false},
		{"0", 0, true},
		{"31", 31, true},
		{"0012", 12, true},
		{"18446744073709551616", maxSGRParameter, true},
		{"99999999999999999999", maxSGRParameter, true},
		{"4294967296", maxSGRParameter, true},
		{"12a", 0, false},
		{"-1", 0, false},
	} {
		got, ok := parseSGRNumber(test.in)
		if got != test.want || ok != test.ok {
			t.Errorf("parseSGRNumber(%q) = %d, %v, want %d, %v", test.in, got, ok, test.want, test.ok)
		}
	}
}

// A wrapped field must never reach the reset code, whatever the digit string.
func TestParseSGRNumberNeverWrapsToReset(t *testing.T) {
	fields := []string{
		"18446744073709551616", "36893488147419103232", "92233720368547758080",
		"100000000000000000000", "340282366920938463463374607431768211456",
	}
	for _, field := range fields {
		if got, _ := parseSGRNumber(field); got == 0 {
			t.Errorf("parseSGRNumber(%q) = 0, which is the reset code", field)
		}
	}
}
