package tui

import "testing"

// colors.ts interpolates the rejected channel with `${value}`, i.e.
// String(number): fixed notation up to 1e21 and from 1e-6, so 1e21% lightness
// reads 10000000000000000000 rather than Go's 1e+19 (pi v1.5.0
// packages/tui/src/colors.ts parseColor, oklchColor, okhslColor).
func TestColorErrorsFormatNumbersLikeJavaScript(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"oklch(1e21% 0.4 1)", "l must be between 0 and 1: 10000000000000000000"},
		{"oklch(0.5 -0.00001 1)", "c must not be negative: -0.00001"},
		{"okhsl(0 -1e-7 0.5)", "s must be between 0 and 1: -1e-7"},
		{"okhsl(0 0.5 150%)", "l must be between 0 and 1: 1.5"},
	} {
		_, err := ParseColor(tc.value)
		if err == nil || err.Error() != tc.want {
			t.Errorf("ParseColor(%q) error = %v; want %s", tc.value, err, tc.want)
		}
	}
}
