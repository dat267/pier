package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Reference: pi v1.4.0 (4a080252), oauth-selector.ts filterProviders includes
// provider.method.name. The real /login options carry *ai.OAuthAuth methods.
func TestOAuthSelectorSearchesAuthMethodName(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	providers := wiring.GetLoginProviderOptions("")
	for _, tc := range []struct {
		query string
		match bool
	}{
		{"ChatGPT", true},
		{"OpenAI", true},
		{"not-a-real-login-method", false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			selected := ""
			selector := NewOAuthSelectorComponent("login", providers,
				func(id, authType string) { selected = id }, nil, tc.query)
			selector.HandleInput("\r")
			if (selected != "") != tc.match {
				rendered := tui.StripTerminalSequences(strings.Join(selector.Render(80), "\n"))
				t.Fatalf("query %q selected %q, want match=%v; rendered:\n%s", tc.query, selected, tc.match, rendered)
			}
		})
	}
}
