package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 packages/tui/src/keys.ts parseKey, executed over legacy, Kitty,
// function-key, mouse, modifier and astral sequences in both protocol modes.
// Regenerate:
// node tui/testdata/parse-key/generate.mjs ./pi > tui/testdata/parse-key/golden.json
func TestParseKeyMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/parse-key/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string  `json:"name"`
		Sequence string  `json:"sequence"`
		Kitty    bool    `json:"kitty"`
		Expected *string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty parse-key corpus")
	}
	previous := IsKittyProtocolActive()
	t.Cleanup(func() { SetKittyProtocolActive(previous) })
	for _, test := range cases {
		SetKittyProtocolActive(test.Kitty)
		got, ok := ParseKey(test.Sequence)
		want := ""
		if test.Expected != nil {
			want = *test.Expected
		}
		if (test.Expected == nil) == ok || got != want {
			name := test.Name
			t.Errorf("%s: ParseKey = (%q, %v), want %q present=%v", name, got, ok, want, test.Expected != nil)
		}
	}
	SetKittyProtocolActive(previous)
}
