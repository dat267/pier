package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D460: text an authorization server supplies is data, not terminal control.
// The reference prints the device user code, the instructions and an info
// message as they arrive, so an ESC sequence in any of them reaches the
// terminal; pier treats remote text like every other provider string (D342).
func TestAuthLoginNotifyDropsProviderTerminalControl(t *testing.T) {
	terminal := newLoginTestTerminal(false)
	handle := newLoginPromptHandler("pier", authLoginRequest{providerRef: "xai"}, ai.CredentialOAuth, "xai", terminal)

	handle.notify(ai.AuthEvent{Type: ai.AuthEventDeviceCode, VerificationURI: "https://x.ai/device", UserCode: "\x1b]0;owned\x07ABCD-1234"})
	handle.notify(ai.AuthEvent{Type: ai.AuthEventAuthURL, URL: "https://x.ai/authorize", Instructions: "line one\n\x1b[2Jline two"})
	handle.notify(ai.AuthEvent{Type: ai.AuthEventInfo, Message: "\x1b]8;;https://evil\x07click", Links: []ai.AuthInfoLink{{URL: "https://x.ai/help", Label: "\x1b[31mhelp\x1b[0m"}}})

	output := terminal.output()
	if strings.ContainsAny(output, "\x1b\x07") {
		t.Fatalf("provider control characters reached the terminal: %q", output)
	}
	for _, want := range []string{"ABCD-1234", "line one", "line two", "click", "help"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output dropped %q: %q", want, output)
		}
	}
}
