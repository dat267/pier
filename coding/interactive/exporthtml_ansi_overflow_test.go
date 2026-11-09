package interactive

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365)
// packages/coding-agent/src/core/export-html/ansi-to-html.ts.
//
// The converter parses parameters with `parseInt(p, 10) || 0`. A parameter
// wider than 2^53 parses to a huge Number, which is truthy and matches no SGR
// branch, so the styling stays. The port's strconv.Atoi fails on such a
// parameter and stored 0, which reset every attribute.
func TestAnsiToHTMLIgnoresOutOfRangeSGRParameter(t *testing.T) {
	for _, parameter := range []string{
		"18446744073709551616",
		"99999999999999999999",
		"9223372036854775808",
	} {
		got := AnsiToHTML("\x1b[31mred\x1b[" + parameter + "mplain")
		want := `<span style="color:#800000">red</span><span style="color:#800000">plain</span>`
		if got != want {
			t.Errorf("parameter %s: got %q, want %q", parameter, got, want)
		}
	}
}

// An out-of-range 256-color index is still a palette lookup in the reference
// (color256ToHex clamps nothing), so only a parameter that no SGR branch reads
// is ignored.
func TestAnsiToHTMLKeepsStylesAcrossOutOfRangeParameter(t *testing.T) {
	got := AnsiToHTML("\x1b[1;4mstyled\x1b[18446744073709551616mplain")
	want := `<span style="font-weight:bold;text-decoration:underline">styled</span><span style="font-weight:bold;text-decoration:underline">plain</span>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "plain</span>") == false {
		t.Errorf("got %q, want the styling to continue over the tail", got)
	}
}
