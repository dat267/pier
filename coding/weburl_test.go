package coding

import "testing"

// replaceControlCharacterRuns replaces a run of C0 *or* C1 controls with one
// space (core/utils/replace-control-character-runs.ts). A sanitizer that only
// handled C0 would leave the C1 block (0x7f-0x9f, including NEL and the CSI
// single-character introducer) in place.
func TestReplaceControlCharacterRuns(t *testing.T) {
	for _, testCase := range []struct{ name, input, want string }{
		{"plain text", "hello", "hello"},
		{"C0 run", "a\x00\x01\x02b", "a b"},
		{"C1 run", "a\u0085\u009bb", "a b"},
		{"delete", "a\x7fb", "a b"},
		{"mixed run", "a\x01\u009b\x7fb", "a b"},
		{"separate runs", "a\x01b\x02c", "a b c"},
		{"unicode kept", "a\u00e9b", "a\u00e9b"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ReplaceControlCharacterRuns(testCase.input); got != testCase.want {
				t.Fatalf("ReplaceControlCharacterRuns(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

// webUrl parses with WHATWG URL, which trims C0 controls and spaces from the
// ends, drops tabs and newlines and percent-encodes other controls before
// parsing (pi v1.6.0, faac5858, utils/web-url.ts). Go's url.Parse rejects any
// control character, so a login URL read with its trailing newline was refused.
func TestWebURLPreprocessesLikeWHATWG(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://x.y/\n", "https://x.y/"},
		{"  https://x.y/a\r\n", "https://x.y/a"},
		{"https://x.y/a\tb", "https://x.y/ab"},
		{"https://x.y/\x1bz", "https://x.y/%1Bz"},
		{"\x00https://x.y/", "https://x.y/"},
	} {
		if got, ok := WebURL(tc.in); !ok || got != tc.want {
			t.Errorf("WebURL(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
}
