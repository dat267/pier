package coding

import (
	"bytes"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) main.ts runAuthCommand passes
// args.slice(2).filter((arg) => arg !== "--offline") to the login and logout
// commands, because main already applied offline mode from the raw argv. Pier
// forwarded the flag, so `pi --offline auth login <provider>` and
// `pi auth logout --offline <provider>` failed on it as an extra argument.
func TestAuthLoginAndLogoutIgnoreTheOfflineFlag(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	var out, errOut bytes.Buffer
	handled, code := RunAuthCommandNamed("pier", []string{"auth", "logout", "--offline", "definitely-not-a-provider"}, &out, &errOut)
	if !handled || code != 1 {
		t.Fatalf("handled = %v code = %d", handled, code)
	}
	if strings.Contains(errOut.String(), "Usage:") || !strings.Contains(errOut.String(), `Unknown provider "definitely-not-a-provider".`) {
		t.Fatalf("logout stderr = %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	handled, code = RunAuthCommandNamed("pier", []string{"auth", "login", "--offline", "anthropic"}, &out, &errOut)
	if !handled || code != 1 {
		t.Fatalf("handled = %v code = %d", handled, code)
	}
	if strings.Contains(errOut.String(), "Usage:") || !strings.Contains(errOut.String(), "requires an interactive terminal") {
		t.Fatalf("login stderr = %q", errOut.String())
	}
}
