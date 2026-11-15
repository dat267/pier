package coding

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) cli/auth-command.ts printAuthCommandHelp
// prints the usage block, a description, and the Commands / login / other
// option sections. The port printed the usage block and one sentence, so
// `pi auth --help` lost the per-command and per-option documentation.
func TestPrintAuthCommandHelpMatchesTheReference(t *testing.T) {
	help := PrintAuthCommandHelp("pier")
	for _, want := range []string{
		"pier auth login <provider> [--api-key] [--no-browser]",
		"Log in to and out of model providers, print their credentials for external clients, or check that they are ready.",
		"Credentials are stored in ~/.pi/agent/auth.json.",
		"Commands:",
		"login <provider>        Sign in with an account in the browser, or save an API key, then exit",
		"logout <provider>       Remove the stored credentials; environment variables and models.json stay",
		"check                   Print ready, not_ready, or invalid; exit with 0, 1, or 2",
		"Options for print-api-key, print-bearer-token, and check (at least one is required):",
		"Options for login:",
		"--no-browser            Do not open a browser; use another sign-in method, e.g. device code",
		"Other options:",
		"--min-expiry <duration> Minimum remaining token lifetime for print-bearer-token, e.g. 30m or 1h",
		"--credentials           Emit the credential from check, or include it in JSON output",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("auth help is missing %q:\n%s", want, help)
		}
	}
}
