package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// scriptedLoginTerminal answers prompts from a list and records the output.
type scriptedLoginTerminal struct {
	answers     []string
	writes      []string
	errors      []string
	opened      []string
	interactive bool
}

func (t *scriptedLoginTerminal) Write(line string)     { t.writes = append(t.writes, line) }
func (t *scriptedLoginTerminal) Error(line string)     { t.errors = append(t.errors, line) }
func (t *scriptedLoginTerminal) Interactive() bool     { return t.interactive }
func (t *scriptedLoginTerminal) OpenURL(target string) { t.opened = append(t.opened, target) }

func (t *scriptedLoginTerminal) answer() (string, error) {
	if len(t.answers) == 0 {
		return "", errors.New("no scripted answer left")
	}
	answer := t.answers[0]
	t.answers = t.answers[1:]
	return answer, nil
}

func (t *scriptedLoginTerminal) Question(_ context.Context, message string) (string, error) {
	t.writes = append(t.writes, message)
	return t.answer()
}

func (t *scriptedLoginTerminal) Secret(_ context.Context, message string) (string, error) {
	t.writes = append(t.writes, message)
	return t.answer()
}

func (t *scriptedLoginTerminal) output() string { return strings.Join(t.writes, "\n") }

func newLoginTestTerminal(interactive bool, answers ...string) *scriptedLoginTerminal {
	return &scriptedLoginTerminal{answers: answers, interactive: interactive}
}

// pi v1.5.2 (b4afdddf) cli/auth-login.ts: `pi auth login <provider> --api-key`
// asks for the key, stores it and exits.
func TestAuthLoginSavesAnAPIKey(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	terminal := newLoginTestTerminal(true, "sk-ant-test")

	code := RunAuthLogin("pier", []string{"anthropic", "--api-key"}, terminal, agentDir, agentDir, func() string { return "device" })
	if code != 0 {
		t.Fatalf("exit = %d, errors = %v", code, terminal.errors)
	}
	if !strings.Contains(terminal.output(), "Saved API key for Anthropic. Credentials saved to "+filepath.Join(agentDir, "auth.json")) {
		t.Fatalf("output = %q", terminal.output())
	}
	if !strings.Contains(terminal.output(), "Enter Anthropic API key") {
		t.Fatalf("the key prompt was not shown: %q", terminal.output())
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if entry, ok := stored["anthropic"]; !ok || entry.Type != "api_key" || entry.Key != "sk-ant-test" {
		t.Fatalf("auth.json = %s", data)
	}
}

// Usage, unknown-provider and non-interactive failures mirror the reference's
// messages and exit codes.
func TestAuthLoginRejectsBadInvocations(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)

	cases := []struct {
		name        string
		args        []string
		interactive bool
		want        string
	}{
		{name: "no-provider", args: nil, interactive: true, want: "Usage: pier auth login <provider> [--api-key] [--no-browser]"},
		{name: "extra-argument", args: []string{"anthropic", "openai"}, interactive: true, want: "Usage: pier auth login"},
		{name: "api-key-with-no-browser", args: []string{"anthropic", "--api-key", "--no-browser"}, interactive: true,
			want: "--no-browser only applies to account sign-in; omit it with --api-key."},
		{name: "unknown-provider", args: []string{"nope"}, interactive: true, want: `Unknown provider "nope".`},
		{name: "not-a-terminal", args: []string{"anthropic", "--api-key"}, interactive: false, want: `requires an interactive terminal`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			terminal := newLoginTestTerminal(testCase.interactive, "sk-ant-test")
			code := RunAuthLogin("pier", testCase.args, terminal, agentDir, agentDir, func() string { return "device" })
			if code != 1 {
				t.Fatalf("exit = %d, errors = %v", code, terminal.errors)
			}
			if !strings.Contains(strings.Join(terminal.errors, "\n"), testCase.want) {
				t.Fatalf("errors = %v; want %q", terminal.errors, testCase.want)
			}
		})
	}
}

// The method selection follows the reference's createLoginInteraction: an API key
// setup asks every selection, the browser choice takes the first option and
// records the rest for the hint, and the "other" choice uses a single
// alternative without asking.
func TestAuthLoginSelectsTheSignInMethod(t *testing.T) {
	options := []ai.AuthSelectOption{
		{ID: "browser", Label: "Sign in with an account in the browser"},
		{ID: "paste", Label: "Paste a redirect URL"},
	}
	selectPrompt := ai.AuthPrompt{Type: ai.AuthPromptSelect, Message: "How do you want to sign in?", SelectOptions: options}

	// An API key setup asks: the scripted answer picks the second option.
	asking := newLoginTestTerminal(true, "2")
	handle := newLoginPromptHandler("pier", authLoginRequest{}, ai.CredentialAPIKey, "anthropic", asking)
	if got, err := handle.prompt(selectPrompt); err != nil || got != "paste" {
		t.Fatalf("ask = %q, %v", got, err)
	}
	if !strings.Contains(asking.output(), "Select 1-2: ") {
		t.Fatalf("prompt = %q", asking.output())
	}

	// The browser default takes the first option and records the alternative.
	browser := newLoginTestTerminal(true)
	handle = newLoginPromptHandler("pier", authLoginRequest{providerRef: "anthropic", browser: true}, ai.CredentialOAuth, "anthropic", browser)
	if got, err := handle.prompt(selectPrompt); err != nil || got != "browser" {
		t.Fatalf("browser = %q, %v", got, err)
	}
	handle.notify(ai.AuthEvent{Type: ai.AuthEventAuthURL, URL: "https://claude.ai/oauth/authorize?code=1"})
	if !strings.Contains(browser.output(), `To use "Paste a redirect URL" instead, run "pier auth login anthropic --no-browser"`) {
		t.Fatalf("hint = %q", browser.output())
	}
	if len(browser.opened) != 1 || browser.opened[0] != "https://claude.ai/oauth/authorize?code=1" {
		t.Fatalf("opened = %v", browser.opened)
	}

	// Without --no-browser's default, a single alternative is used directly.
	other := newLoginTestTerminal(true)
	handle = newLoginPromptHandler("pier", authLoginRequest{providerRef: "anthropic", browser: false}, ai.CredentialOAuth, "anthropic", other)
	if got, err := handle.prompt(selectPrompt); err != nil || got != "paste" {
		t.Fatalf("other = %q, %v", got, err)
	}
	if !strings.Contains(other.output(), "Paste a redirect URL") {
		t.Fatalf("other output = %q", other.output())
	}

	// A non-web URL is shown as text and never opened.
	noWeb := newLoginTestTerminal(true)
	handle = newLoginPromptHandler("pier", authLoginRequest{providerRef: "anthropic", browser: true}, ai.CredentialOAuth, "anthropic", noWeb)
	handle.notify(ai.AuthEvent{Type: ai.AuthEventAuthURL, URL: "javascript:\x00alert(1)"})
	if len(noWeb.opened) != 0 {
		t.Fatalf("opened a non-web URL: %v", noWeb.opened)
	}
	if !strings.Contains(noWeb.output(), "javascript: alert(1)") {
		t.Fatalf("display = %q", noWeb.output())
	}
}

// `pier auth login` is a recognized subcommand: it reaches its own argument
// validation instead of the unknown-command guidance that used to name it.
func TestAuthLoginIsARecognizedSubcommand(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommandNamed("pier", []string{"auth", "login", "anthropic", "--api-key", "--no-browser"}, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d stderr=%q", handled, code, stderr.String())
	}
	if strings.Contains(stderr.String(), "Unknown auth command") {
		t.Fatalf("login is not recognized:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--no-browser only applies to account sign-in") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
