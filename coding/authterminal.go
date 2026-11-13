package coding

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// AuthOutput is the output shared by the terminal authentication commands
// (pi v1.5.2 cli/auth-terminal.ts AuthOutput).
type AuthOutput interface {
	Write(line string)
	Error(line string)
}

// LoginTerminal is the line-based terminal I/O `pier auth login` needs. Its
// prompts return an error carrying "Login cancelled" when ctx is done.
type LoginTerminal interface {
	AuthOutput
	// Interactive reports whether stdin and stdout are a terminal; login
	// prompts need one.
	Interactive() bool
	Question(ctx context.Context, message string) (string, error)
	// Secret reads a line without echoing it.
	Secret(ctx context.Context, message string) (string, error)
	OpenURL(target string)
}

// ProcessLoginTerminal reads a line at a time from stdin and writes to the given
// writers, hiding secrets when stdin is a terminal.
type ProcessLoginTerminal struct {
	Reader              *bufio.Reader
	Out                 io.Writer
	Err                 io.Writer
	InteractiveTerminal bool
	// SecretFD is the terminal file descriptor secrets are read from; a
	// negative value reads a plain line instead.
	SecretFD int
}

// AuthLoginCancelledMessage is the error text of a cancelled prompt
// (upstream rejects with Error("Login cancelled")).
const AuthLoginCancelledMessage = "Login cancelled"

// NewProcessLoginTerminal builds the terminal for a process: stdin/stdout/stderr
// and, when stdin is a terminal, hidden secret input from fd 0.
func NewProcessLoginTerminal(stdin io.Reader, stdout, stderr io.Writer, interactive bool, secretFD int) *ProcessLoginTerminal {
	return &ProcessLoginTerminal{
		Reader: bufio.NewReader(stdin), Out: stdout, Err: stderr,
		InteractiveTerminal: interactive, SecretFD: secretFD,
	}
}

// Write prints one line.
func (t *ProcessLoginTerminal) Write(line string) { _, _ = fmt.Fprintln(t.Out, line) }

// Error prints one line to the error stream.
func (t *ProcessLoginTerminal) Error(line string) { _, _ = fmt.Fprintln(t.Err, line) }

// Interactive reports whether the terminal can prompt.
func (t *ProcessLoginTerminal) Interactive() bool { return t.InteractiveTerminal }

// Question prints message and reads one line.
func (t *ProcessLoginTerminal) Question(ctx context.Context, message string) (string, error) {
	if err := ctxErrOf(ctx); err != nil {
		return "", fmt.Errorf("%s", AuthLoginCancelledMessage)
	}
	_, _ = fmt.Fprint(t.Out, message)
	text, err := t.Reader.ReadString('\n')
	if err != nil && (text == "" || err != io.EOF) {
		if ctxErrOf(ctx) != nil || err == io.EOF {
			return "", fmt.Errorf("%s", AuthLoginCancelledMessage)
		}
		return "", err
	}
	return strings.TrimRight(text, "\r\n"), nil
}

// Secret prints message and reads one line without echo when a terminal fd is
// available, else like Question.
func (t *ProcessLoginTerminal) Secret(ctx context.Context, message string) (string, error) {
	if err := ctxErrOf(ctx); err != nil {
		return "", fmt.Errorf("%s", AuthLoginCancelledMessage)
	}
	if t.SecretFD < 0 || !term.IsTerminal(t.SecretFD) {
		return t.Question(ctx, message)
	}
	_, _ = fmt.Fprint(t.Out, message)
	secret, err := term.ReadPassword(t.SecretFD)
	_, _ = fmt.Fprintln(t.Out)
	if err != nil {
		return "", err
	}
	return string(secret), nil
}

// OpenURL opens a normalized web target in the browser.
func (t *ProcessLoginTerminal) OpenURL(target string) { OpenBrowser(target) }

// isInteractiveTerminal reports whether both ends of the process are a terminal.
func isInteractiveTerminal(stdin io.Reader, stdout io.Writer) bool {
	inFile, inOK := stdin.(*os.File)
	outFile, outOK := stdout.(*os.File)
	if !inOK || !outOK {
		return false
	}
	return term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
}

// terminalSecretFD is the descriptor secrets are read from, or -1 when stdin is
// not a terminal.
func terminalSecretFD(stdin io.Reader) int {
	if inFile, ok := stdin.(*os.File); ok && term.IsTerminal(int(inFile.Fd())) {
		return int(inFile.Fd())
	}
	return -1
}
