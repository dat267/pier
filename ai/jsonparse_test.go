package ai

import (
	"strings"
	"testing"
)

// Upstream v1.3.0 parseToolCallArguments (#10944): a finished tool call whose
// complete arguments text is not a JSON object keeps no parsed prefix; the
// arguments become {} and the error carries the text the model sent.
func TestParseToolCallArguments(t *testing.T) {
	valid, errText := ParseToolCallArguments(`{"a":1}`)
	if errText != "" || string(valid) != `{"a":1}` {
		t.Fatalf("valid = %s, %q", valid, errText)
	}
	empty, errText := ParseToolCallArguments("")
	if errText != "" || string(empty) != `{}` {
		t.Fatalf("empty = %s, %q", empty, errText)
	}
	for _, malformed := range []string{`{`, `{"a":}`, `[1,2]`, `"text"`, `null`, `not json`} {
		args, errText := ParseToolCallArguments(malformed)
		if errText == "" {
			t.Errorf("ParseToolCallArguments(%q) did not report an error", malformed)
			continue
		}
		if string(args) != `{}` {
			t.Errorf("ParseToolCallArguments(%q) arguments = %s; want {}", malformed, args)
		}
		if !strings.Contains(errText, "Received arguments:\n"+malformed) {
			t.Errorf("ParseToolCallArguments(%q) error = %q", malformed, errText)
		}
	}
}
