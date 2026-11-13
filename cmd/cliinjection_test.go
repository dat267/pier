package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// The CLI prints to a terminal too, before any renderer exists, so the same
// rule applies as in coding/interactive/renderinjection_test.go: text that came
// from configuration or a remote peer is data (D342, D460-D464). Add a subtest
// here for every new command that prints such text; the wiring-level runs that
// need a process live beside their own subject
// (TestMCPConfigErrorsKeepTerminalControlsOutOfStderr,
// TestPrintModeProviderErrorKeepsTerminalControlsOut).
func TestCLIOutputKeepsTerminalControlsOut(t *testing.T) {
	hostile := "evil\x1b]0;owned\x07\x1b[2J"

	t.Run("model list", func(t *testing.T) {
		isolatedAgentDir(t)
		agentDir := coding.GetAgentDir()
		// A models.json provider id is not validated, so both columns are reachable.
		models := `{"providers":{"evil\u001b]0;owned\u0007\u001b[2J":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"k","models":[` +
			`{"id":"model\u001b]0;owned\u0007\u001b[2J","contextWindow":1000,"maxTokens":100}]}}}`
		if err := os.WriteFile(filepath.Join(agentDir, "models.json"), []byte(models), 0o600); err != nil {
			t.Fatal(err)
		}
		runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{AllowModelNetwork: false})
		if err != nil {
			t.Fatal(err)
		}
		settings := coding.NewSettingsManagerFromFiles(t.TempDir(), agentDir, coding.SettingsManagerCreateOptions{})
		stdout, stderr := captureStdio(t, func() {
			if err := listModels(runtime, settings, coding.ParseArgs(nil), context.Background()); err != nil {
				t.Errorf("listModels: %v", err)
			}
		})
		assertOnlyCLIEscapes(t, "model list", stdout+stderr)
		if !strings.Contains(stdout, "evil") || !strings.Contains(stdout, "model") {
			t.Fatalf("the table lost the identifiers: %q", stdout)
		}
	})

	t.Run("auth logout", func(t *testing.T) {
		agentDir := t.TempDir()
		t.Setenv("PI_CODING_AGENT_DIR", agentDir)
		models := `{"providers":{"evil":{"name":"Evil\u001b]0;owned\u0007Corp","baseUrl":"https://evil.example/v1","api":"openai-completions","apiKey":"k","models":[{"id":"m"}]}}}`
		if err := os.WriteFile(filepath.Join(agentDir, "models.json"), []byte(models), 0o600); err != nil {
			t.Fatal(err)
		}
		storage := coding.NewAuthStorage(filepath.Join(agentDir, "auth.json"))
		if _, err := storage.Modify(hostile, func(*ai.Credential) (*ai.Credential, error) {
			return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-test"}}, nil
		}, context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, arg := range []string{"evil", "nosuch"} {
			var stdout, stderr strings.Builder
			coding.RunAuthCommandNamed("pier", []string{"auth", "logout", arg}, &stdout, &stderr)
			assertOnlyCLIEscapes(t, "auth logout "+arg, stdout.String()+stderr.String())
		}
	})
}

// assertOnlyCLIEscapes fails when CLI output carries an escape sequence the CLI
// did not build itself: only SGR (its own colors) is allowed.
func assertOnlyCLIEscapes(t *testing.T, label, output string) {
	t.Helper()
	if err := onlyCLIEscapes(output); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func onlyCLIEscapes(output string) error {
	for index := 0; index < len(output); index++ {
		if output[index] != 0x1b {
			continue
		}
		rest := output[index+1:]
		if !strings.HasPrefix(rest, "[") {
			return errAt(output, index)
		}
		end := strings.IndexFunc(rest[1:], func(r rune) bool { return r >= 0x40 && r <= 0x7e })
		if end < 0 || rest[end+1] != 'm' {
			return errAt(output, index)
		}
		index += end + 1
	}
	return nil
}

func errAt(output string, index int) error {
	end := index + 1 + 8
	if end > len(output) {
		end = len(output)
	}
	return &cliEscapeError{seq: output[index:end]}
}

type cliEscapeError struct{ seq string }

func (e *cliEscapeError) Error() string {
	return "printed a non-CLI escape sequence " + quoteSeq(e.seq)
}

func quoteSeq(seq string) string {
	return "\"" + strings.ReplaceAll(seq, "\x1b", "ESC") + "\""
}

// TestCLIEscapeWalkerRejectsInjectedFraming keeps the walker above honest.
func TestCLIEscapeWalkerRejectsInjectedFraming(t *testing.T) {
	if err := onlyCLIEscapes("\x1b[31mError: boom\x1b[39m\n"); err != nil {
		t.Fatalf("rejected the CLI's own color: %v", err)
	}
	if err := onlyCLIEscapes("plain text\n"); err != nil {
		t.Fatalf("rejected plain text: %v", err)
	}
	for _, hostile := range []string{"\x1b]0;title\x07", "\x1b[2J", "\x1bP1;2;3\x1b\\", "\x1b]8;;https://a\x1b\\"} {
		if err := onlyCLIEscapes("before " + hostile + " after"); err == nil {
			t.Fatalf("accepted injected framing %q", hostile)
		}
	}
}
