package coding

import (
	"bytes"
	"encoding/json"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupAuthCommandDir points the agent dir at a temp dir and seeds auth.json.
func setupAuthCommandDir(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	writeAuthFile(t, filepath.Join(dir, "auth.json"), content)
}

func TestRunAuthCommandPrintAPIKey(t *testing.T) {
	setupAuthCommandDir(t, `{"anthropic":{"type":"api_key","key":"sk-secret"}}`)
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommand([]string{"auth", "print-api-key", "--provider", "anthropic"}, &stdout, &stderr)
	if !handled {
		t.Fatal("auth was not handled")
	}
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if stdout.String() != "sk-secret\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunAuthCommandPrintBearerToken(t *testing.T) {
	setupAuthCommandDir(t, `{"openai-codex":{"type":"oauth","refresh":"r","access":"bearer-token","expires":4102444800000}}`)
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommand([]string{"auth", "print-bearer-token", "--provider", "openai-codex"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v exit=%d stderr=%s", handled, code, stderr.String())
	}
	if stdout.String() != "bearer-token\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunAuthCommandPrintAPIKeyRejectsOAuth(t *testing.T) {
	setupAuthCommandDir(t, `{"openai-codex":{"type":"oauth","refresh":"r","access":"bearer-token","expires":4102444800000}}`)
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommand([]string{"auth", "print-api-key", "--provider", "openai-codex"}, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("handled=%v exit=%d", handled, code)
	}
	if !strings.Contains(stderr.String(), "configured with OAuth, not an API key") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunAuthCommandCheckJSON(t *testing.T) {
	setupAuthCommandDir(t, `{"anthropic":{"type":"api_key","key":"sk-secret"}}`)
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommand([]string{"auth", "check", "--provider", "anthropic", "--json"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v exit=%d stderr=%s", handled, code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, stdout.String())
	}
	if result["status"] != "ready" || result["provider"] != "anthropic" || result["authType"] != "api_key" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunAuthCommandCheckNotReady(t *testing.T) {
	setupAuthCommandDir(t, `{"openai":{"type":"api_key","key":"sk-other"}}`)
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommand([]string{"auth", "check", "--provider", "anthropic"}, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("handled=%v exit=%d", handled, code)
	}
	if strings.TrimSpace(stdout.String()) != "not_ready" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunAuthCommandHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommand([]string{"auth", "help"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v exit=%d", handled, code)
	}
	if !strings.Contains(stdout.String(), "print-api-key") || !strings.Contains(stdout.String(), "auth check") {
		t.Fatalf("help = %q", stdout.String())
	}
}

func TestRunAuthCommandIgnoresNonAuth(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, _ := RunAuthCommand([]string{"hello", "world"}, &stdout, &stderr)
	if handled {
		t.Fatal("a non-auth invocation must not be handled")
	}
}

// The auth help names the invoked binary, like PrintHelpNamed: installed as
// "pier", it must not print a command that says "pi". `pi auth` is not a
// substring of `pier auth`, so the negative check is exact.
func TestRunAuthCommandHelpUsesTheInvokedName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommandNamed("pier", []string{"auth", "--help"}, &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d stderr=%s", handled, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pier auth print-api-key") {
		t.Errorf("the help does not name the invoked binary:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "pi auth") {
		t.Errorf("the help names the upstream binary:\n%s", stdout.String())
	}
}

// pi v1.4.0 main.ts runAuthCommand reports unknownFlags.keys().next(), preserving argv order.
func TestRunAuthCommandReportsFirstUnknownOption(t *testing.T) {
	for attempt := range 32 {
		var stdout, stderr bytes.Buffer
		handled, code := RunAuthCommandNamed("pier", []string{
			"auth", "check", "--provider", "anthropic", "--first", "--second",
		}, &stdout, &stderr)
		if !handled || code != 1 {
			t.Fatalf("handled=%v code=%d stderr=%s", handled, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), `Unknown option --first for "auth check".`) {
			t.Fatalf("attempt %d reported a non-first option:\n%s", attempt, stderr.String())
		}
	}
}

// pi v1.4.0 packages/coding-agent/src/cli/auth-command.ts uses APP_NAME in
// unknown-subcommand guidance; the port substitutes the invoked binary name.
func TestRunAuthCommandUnknownSubcommandUsesTheInvokedName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommandNamed("pier", []string{"auth", "unknown"}, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d stderr=%s", handled, code, stderr.String())
	}
	// pi v1.5.2 lists login, logout, print-api-key, print-bearer-token and check.
	for _, subcommand := range []string{"login", "logout", "print-api-key", "print-bearer-token", "check"} {
		if !strings.Contains(stderr.String(), `"pier auth `+subcommand+`"`) {
			t.Fatalf("unknown-command guidance omits %q:\n%s", subcommand, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), `Use "pi auth`) {
		t.Fatalf("unknown-command guidance does not name the invoked binary:\n%s", stderr.String())
	}
}

// The unknown-option hint names the invoked binary too.
func TestRunAuthCommandUnknownOptionUsesTheInvokedName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommandNamed("pier", []string{"auth", "check", "--nope"}, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d stderr=%s", handled, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `Use "pier --help"`) {
		t.Errorf("the unknown-option hint does not name the invoked binary:\n%s", stderr.String())
	}
}

// Upstream matches the raw arg with /^(\d+)(ms|s|m|h)$/iu, so surrounding
// whitespace is rejected while the unit stays case-insensitive.
// pi v1.4.0 packages/coding-agent/src/cli/auth-command.ts multiplies as a JS
// Number and credential-print.ts forwards the result. Saturating at int64 max
// preserves this duration's effective impossible-to-satisfy threshold.
func TestParseAuthMinExpiryDoesNotWrap(t *testing.T) {
	milliseconds, ok := parseAuthMinExpiry("3000000000000h")
	if !ok {
		t.Fatal("large but syntactically valid duration was rejected")
	}
	if want := int64(1<<63 - 1); milliseconds != want {
		t.Fatalf("duration = %d ms, want saturated threshold %d", milliseconds, want)
	}
}

func TestParseAuthMinExpiryRejectsWhitespace(t *testing.T) {
	if _, ok := parseAuthMinExpiry(" 30m "); ok {
		t.Fatal("whitespace-padded duration should be rejected")
	}
	// The unit comparison is case-sensitive even though the regex is not, so
	// "30M" falls through to the hours branch (upstream quirk).
	if ms, ok := parseAuthMinExpiry("30M"); !ok || ms != 108000000 {
		t.Fatalf("30M = %d, %v; want 108000000, true", ms, ok)
	}
}

// Reference: pi v1.5.0 cli/auth-check.ts createAuthCheckModelRuntime calls
// ModelRuntime.create with no modelsPath, so the default models.json is loaded;
// a provider declared there is found and its literal, $ENV or !command key
// resolves. Pier passed DisableModelsJSON, so `pi auth check` reported
// provider_not_found for every models.json-only provider that `pi auth
// print-api-key` could resolve.
func TestAuthCheckSeesModelsJSONProviders(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	t.Setenv("PIER_AUTH_TEST_KEY", "env-secret")
	models := `{"providers":{
		"lit":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"dummy","models":[{"id":"l1","name":"L","contextWindow":1000,"maxTokens":100}]},
		"envprov":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"$PIER_AUTH_TEST_KEY","models":[{"id":"e1","name":"E","contextWindow":1000,"maxTokens":100}]},
		"nokey":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","models":[{"id":"n1","name":"N","contextWindow":1000,"maxTokens":100}]}
	}}`
	if err := os.WriteFile(filepath.Join(agentDir, "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args     []string
		status   string
		reason   string
		authType string
		exit     int
	}{
		{[]string{"auth", "check", "--provider", "lit", "--json"}, "ready", "", "api_key", 0},
		{[]string{"auth", "check", "--provider", "envprov", "--json"}, "ready", "", "api_key", 0},
		{[]string{"auth", "check", "--provider", "nokey", "--json"}, "not_ready", "credentials_not_configured", "", 1},
		{[]string{"auth", "check", "--model", "lit/l1", "--json"}, "ready", "", "api_key", 0},
	}
	for _, testCase := range cases {
		var stdout, stderr bytes.Buffer
		handled, code := RunAuthCommandNamed("pier", testCase.args, &stdout, &stderr)
		if !handled {
			t.Fatalf("%v was not handled", testCase.args)
		}
		if code != testCase.exit {
			t.Errorf("%v exit = %d, want %d (stdout %q stderr %q)", testCase.args, code, testCase.exit, stdout.String(), stderr.String())
		}
		var result struct {
			Status   string `json:"status"`
			Provider string `json:"provider"`
			Reason   string `json:"reason"`
			AuthType string `json:"authType"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
			t.Fatalf("%v output %q: %v (stderr %q)", testCase.args, stdout.String(), err, stderr.String())
		}
		if result.Status != testCase.status || result.Reason != testCase.reason || result.AuthType != testCase.authType {
			t.Errorf("%v = %+v, want status %q reason %q authType %q",
				testCase.args, result, testCase.status, testCase.reason, testCase.authType)
		}
		if strings.Contains(stdout.String(), "provider_not_found") {
			t.Errorf("%v reported provider_not_found for a models.json provider", testCase.args)
		}
	}
}

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
