package interactive

import "testing"

// The SGR field reader clamps a field wider than an int at maxSGRParameter
// instead of wrapping. `parseInt(p, 10) || 0` in the reference yields a huge
// Number that matches no SGR branch, so a clamp is the same observable
// outcome; a wrap can land on 0, the reset code (A435).
func TestAnsiSGRParameterClampsWideFields(t *testing.T) {
	for _, test := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"31", 31},
		{"0012", 12},
		{"18446744073709551616", maxSGRParameter},
		{"99999999999999999999", maxSGRParameter},
		{"4294967296", maxSGRParameter},
		{"12a", 0},
		{"-1", 0},
		{" ", 0},
	} {
		if got := ansiSGRParameter(test.in); got != test.want {
			t.Errorf("ansiSGRParameter(%q) = %d, want %d", test.in, got, test.want)
		}
	}
}

// A wrapped field must never reach the reset code, whatever the digit string.
func TestAnsiSGRParameterNeverWrapsToReset(t *testing.T) {
	fields := []string{
		"18446744073709551616", "36893488147419103232", "92233720368547758080",
		"100000000000000000000", "340282366920938463463374607431768211456",
	}
	for _, field := range fields {
		if got := ansiSGRParameter(field); got == 0 {
			t.Errorf("ansiSGRParameter(%q) = 0, which is the reset code", field)
		}
	}
}
