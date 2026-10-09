package coding

import "testing"

// Upstream's `--tools` and `--exclude-tools` entries are names or patterns, where `*` matches any
// run of characters (for example `mcp__radius__*`).
func TestToolNameMatchesPattern(t *testing.T) {
	cases := []struct {
		entry string
		name  string
		want  bool
	}{
		{entry: "read", name: "read", want: true},
		{entry: "read", name: "write", want: false},
		{entry: "mcp__radius__*", name: "mcp__radius__search", want: true},
		{entry: "mcp__radius__*", name: "mcp__other__search", want: false},
		{entry: "mcp__*", name: "mcp__radius__search", want: true},
		{entry: "*__search", name: "mcp__radius__search", want: true},
		{entry: "*", name: "anything", want: true},
		{entry: "re*", name: "read", want: true},
		{entry: "re*d", name: "read", want: true},
		{entry: "re*x", name: "read", want: false},
		{entry: "*a*b*", name: "xaaybz", want: true},
		// A star matches nothing too, so the pattern needs the letters in order.
		{entry: "*a*b*", name: "xb", want: false},
		{entry: "a*b", name: "ab", want: true},
		{entry: "read", name: "mcp__radius__read", want: false},
	}
	for _, testCase := range cases {
		if got := ToolNameMatchesPattern(testCase.entry, testCase.name); got != testCase.want {
			t.Errorf("ToolNameMatchesPattern(%q, %q) = %v, want %v", testCase.entry, testCase.name, got, testCase.want)
		}
	}
}

func TestMatchesAnyToolPattern(t *testing.T) {
	entries := []string{"read", "mcp__radius__*"}
	if !matchesAnyToolPattern(entries, "mcp__radius__search") {
		t.Fatal("a matching pattern was missed")
	}
	if matchesAnyToolPattern(entries, "write") {
		t.Fatal("an unrelated tool matched")
	}
	if matchesAnyToolPattern(nil, "read") {
		t.Fatal("an empty selection matched")
	}
}
