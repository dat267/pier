package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/cli/auth-command.ts
// `/^Bearer\s+(.+)$/iu`: JavaScript's \s is wider than RE2's, so a credential
// header separated by a non-ASCII space is still a Bearer token there.
func TestGetAuthCredentialAcceptsJavaScriptWhitespace(t *testing.T) {
	for _, separator := range []struct {
		name  string
		value string
	}{
		{"ASCII space", " "},
		{"tab", "\t"},
		{"no-break space", "\u00a0"},
		{"ideographic space", "\u3000"},
		{"byte order mark", "\ufeff"},
	} {
		header := "Bearer" + separator.value + "tok-123"
		auth := &ai.AuthResult{Auth: ai.ModelAuth{Headers: ai.ProviderHeaders{
			"Authorization": &header,
		}}}
		if got := GetAuthCredential(auth); got != "tok-123" {
			t.Errorf("%s: GetAuthCredential = %q, want %q", separator.name, got, "tok-123")
		}
	}
	// A non-whitespace separator is not a Bearer token.
	header := "Bearer-tok"
	auth := &ai.AuthResult{Auth: ai.ModelAuth{Headers: ai.ProviderHeaders{"Authorization": &header}}}
	if got := GetAuthCredential(auth); got != "" {
		t.Errorf("GetAuthCredential(non-Bearer) = %q, want empty", got)
	}
}
