package interactive

import (
	"testing"

	"github.com/dat267/pier/coding"
)

// D303: session search compiles `re:` patterns with Go's RE2 engine, not
// JavaScript RegExp. Constructs RE2 lacks are parse errors (no matches);
// supported patterns stay case-insensitive like upstream's "i" flag.
func TestSessionSearchRegexUsesRE2Syntax(t *testing.T) {
	for _, query := range []string{"re:(?=alpha)", `re:(alpha)\1`, "re:[a-"} {
		if parsed := ParseSearchQuery(query); parsed.Error == "" {
			t.Fatalf("%q compiled, want a parse error for a pattern RE2 does not support", query)
		}
	}
	parsed := ParseSearchQuery("re:ALPHA")
	if parsed.Error != "" || parsed.Mode != "regex" || parsed.Regex == nil {
		t.Fatalf("plain pattern did not compile: %+v", parsed)
	}
	result := MatchSession(sessionInfoForSearch("alpha"), parsed)
	if !result.Matches {
		t.Fatal("case-insensitive regex did not match")
	}
}

func sessionInfoForSearch(text string) coding.SessionInfo {
	return coding.SessionInfo{ID: "id", AllMessagesText: text, Cwd: "/tmp"}
}
