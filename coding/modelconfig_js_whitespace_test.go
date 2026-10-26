package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/utils/json.ts
// stripJsonComments. JavaScript's \s includes the Unicode space separators and
// FEFF, so a trailing comma before one of them is stripped there and was not
// here (RE2's \s is ASCII-only).
func TestStripJSONCommentsHandlesJavaScriptWhitespace(t *testing.T) {
	for _, space := range []struct {
		name  string
		value string
	}{
		{"ASCII space", " "},
		{"tab", "\t"},
		{"vertical tab", "\v"},
		{"form feed", "\f"},
		{"no-break space", "\u00a0"},
		{"ogham space mark", "\u1680"},
		{"ideographic space", "\u3000"},
		{"narrow no-break space", "\u202f"},
		{"byte order mark", "\ufeff"},
		{"line separator", "\u2028"},
		{"paragraph separator", "\u2029"},
		{"en quad", "\u2000"},
		{"hair space", "\u200a"},
	} {
		input := `{"providers":{}` + "," + space.value + "}"
		want := `{"providers":{}` + space.value + "}"
		if got := StripJSONComments(input); got != want {
			t.Errorf("%s: StripJSONComments(%q) = %q, want %q", space.name, input, got, want)
		}
	}
	// A non-whitespace neighbour keeps the comma, as in the reference.
	if got := StripJSONComments(`{"providers":{},x}`); got != `{"providers":{},x}` {
		t.Errorf("comma before a non-whitespace character = %q, want it kept", got)
	}
}

// The loader still rejects the file, exactly like the pin: ECMA-404 JSON allows
// only ASCII whitespace, so the leftover separator is a parse error either way.
// This pins that the stripper change above does not start accepting input the
// reference rejects.
func TestLoadModelConfigRejectsUnicodeWhitespaceAfterTrailingComma(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	content := "{\"providers\": {},\u3000}"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config := LoadModelConfig(path)
	if message := config.GetError(); !strings.Contains(message, "Failed to parse models.json") {
		t.Fatalf("LoadModelConfig error = %q, want a parse error like the pin", message)
	}
}
