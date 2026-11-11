package interactive

import "testing"

// TestFooterLineSanitizesStoredCwd pins the footer against terminal control
// injection from a session file (D450). Reference: pi v1.5.0 f9a26365
// modes/interactive/components/footer.ts:165 builds its pwd from
// `formatCwdForFooter(sessionManager.getCwd())` without sanitizing it, while the
// port sanitizes untrusted terminal text (D342, D422). The footer is always
// visible, so a planted session shows the sequences as soon as it is opened,
// with no command required.
func TestFooterLineSanitizesStoredCwd(t *testing.T) {
	line := FooterLine(FooterInput{
		Cwd:         "/tmp/" + storedPayload,
		ModelID:     storedPayload,
		Statuses:    []string{storedPayload},
		ModelWindow: 1000,
	}, 200)
	assertOnlyThemeEscapes(t, []string{line})
}
