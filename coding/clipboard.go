package coding

// Port of packages/coding-agent/src/utils/clipboard.ts and
// clipboard-command.ts: system clipboard copy/read via platform commands,
// with OSC 52 fallback for remote sessions. Upstream also tries a native
// (Bun FFI) clipboard first; the Go port has no native clipboard and relies
// on the platform commands (divergence D106).

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dat267/pier/internal/offloop"
)

// MaxOSC52EncodedLength matches upstream's OSC 52 payload cap.
const MaxOSC52EncodedLength = 100_000

// clipboardMaxBufferBytes is the reference's default maxBufferBytes for a
// clipboard command's collected stdout (50 MiB). A reader that produces more
// is aborted rather than buffered.
const clipboardMaxBufferBytes = 50 * 1024 * 1024

type clipboardCommandOptions struct {
	input     string
	timeoutMS int
	// maxBufferBytes caps collected output like the reference's option; zero
	// means the reference's 50 MiB default.
	maxBufferBytes int
}

// runClipboardCommand runs one clipboard helper. A nil result means the
// command failed; an empty result is a success (upstream runClipboardCommand).
// Writers are not given output pipes so daemons cannot retain them.
func runClipboardCommand(ctx context.Context, command string, args []string, options clipboardCommandOptions) []byte {
	timeoutMS := options.timeoutMS
	if timeoutMS == 0 {
		timeoutMS = 3000
	}
	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, command, args...)
	detachClipboardCommand(cmd)
	if options.input == "" {
		cmd.Stdin = nil
	} else {
		cmd.Stdin = strings.NewReader(options.input)
		cmd.Stdout = nil
	}
	cmd.Stderr = nil
	cmd.Env = os.Environ()
	if options.input == "" {
		// The reference aborts the reader once its collected output exceeds
		// maxBufferBytes (clipboard-command.ts), so reading stops one byte past
		// the cap instead of buffering an unbounded clipboard.
		limit := options.maxBufferBytes
		if limit == 0 {
			limit = clipboardMaxBufferBytes
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil
		}
		if err := cmd.Start(); err != nil {
			return nil
		}
		output, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
		if readErr != nil || len(output) > limit {
			_ = cmd.Process.Kill()
			_ = stdout.Close()
			_ = cmd.Wait()
			return nil
		}
		if err := cmd.Wait(); err != nil {
			return nil
		}
		return output
	}
	if err := cmd.Run(); err != nil {
		return nil
	}
	return []byte{}
}

// isRemoteSession reports whether the copy must also emit OSC 52.
func isRemoteSession() bool { return isRemoteSessionFor(os.Getenv) }

func isRemoteSessionFor(getenv func(string) string) bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("MOSH_CONNECTION") != "" ||
		getenv("PI_CLIPBOARD_OSC52") == "1"
}

func copyCommands() [][]string { return copyCommandsFor(runtime.GOOS, os.Getenv) }

func copyCommandsFor(goos string, getenv func(string) string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	}
	commands := [][]string{}
	if getenv("TERMUX_VERSION") != "" {
		commands = append(commands, []string{"termux-clipboard-set"})
	}
	if getenv("WAYLAND_DISPLAY") != "" {
		commands = append(commands, []string{"wl-copy"})
	}
	if getenv("DISPLAY") != "" {
		commands = append(commands, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return commands
}

func pasteCommands() [][]string { return pasteCommandsFor(runtime.GOOS, os.Getenv) }

func pasteCommandsFor(goos string, getenv func(string) string) [][]string {
	// Termux reports GOOS "android", not "linux", so the Termux reader is
	// selected before the platform gate; the X11/Wayland readers stay
	// linux-only (upstream readClipboardText, #10391).
	commands := [][]string{}
	if getenv("TERMUX_VERSION") != "" {
		commands = append(commands, []string{"termux-clipboard-get"})
	}
	if goos != "linux" {
		return commands
	}
	if getenv("WAYLAND_DISPLAY") != "" {
		commands = append(commands, []string{"wl-paste", "--no-newline", "--type", "text"})
	}
	if getenv("DISPLAY") != "" {
		commands = append(commands, []string{"xclip", "-selection", "clipboard", "-out"}, []string{"xsel", "--clipboard", "--output"})
	}
	return commands
}

func clipboardUnavailableError() error { return clipboardUnavailableErrorFor(runtime.GOOS, os.Getenv) }

func clipboardUnavailableErrorFor(goos string, getenv func(string) string) error {
	// The Termux hint precedes the platform checks: a failed Termux copy on
	// Android must name the Termux:API package, not fall through to the generic
	// message (upstream copyToClipboard, #10391).
	if getenv("TERMUX_VERSION") != "" {
		return fmt.Errorf("Clipboard unavailable: install the Termux:API app and `termux-api` package")
	}
	if goos == "linux" {
		if getenv("WAYLAND_DISPLAY") != "" {
			return fmt.Errorf("Clipboard unavailable: install `wl-clipboard` (`wl-copy`) or check Wayland access")
		}
		if getenv("DISPLAY") != "" {
			return fmt.Errorf("Clipboard unavailable: install `xclip` or `xsel`, or check X11 access")
		}
		return fmt.Errorf("Clipboard unavailable: no Wayland or X11 display detected")
	}
	return fmt.Errorf("Clipboard unavailable")
}

// CopyTextToClipboard copies text to the system clipboard (upstream
// copyToClipboard): platform command writers, then OSC 52 for remote
// sessions.
func CopyTextToClipboard(text string) error {
	return CopyTextToClipboardContext(context.Background(), text)
}

// CopyTextToClipboardContext cancels clipboard subprocesses with the caller's
// lifetime. D200: cancellation must not fall through to OSC 52 output.
func CopyTextToClipboardContext(ctx context.Context, text string) error {
	return CopyTextToClipboardWithOSC52(ctx, text, func(_ context.Context, data string) error {
		if _, err := os.Stdout.WriteString(data); err != nil {
			return clipboardUnavailableError()
		}
		return nil
	})
}

// clipboardCommandRunner runs one helper; nil means the command failed.
type clipboardCommandRunner func(context.Context, string, []string, clipboardCommandOptions) []byte

// CopyTextToClipboardWithOSC52 retains platform command order, but sends the
// complete OSC 52 fallback packet through the caller's output sink. D202: the
// interactive sink shares terminal ordering and optional-output admission.
func CopyTextToClipboardWithOSC52(ctx context.Context, text string, write func(context.Context, string) error) error {
	return copyTextToClipboardWith(ctx, runtime.GOOS, os.Getenv, runClipboardCommand, os.ReadFile, write, text)
}

// copyTextToClipboardWith ports upstream copyToClipboard: platform command
// writers, the WSL Windows fallback, then OSC 52 for remote and headless
// sessions. The native (Bun FFI) writer has no Go counterpart (D106).
func copyTextToClipboardWith(ctx context.Context, goos string, getenv func(string) string, run clipboardCommandRunner, readFile func(string) ([]byte, error), write func(context.Context, string) error, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copied := false
	for _, command := range copyCommandsFor(goos, getenv) {
		if run(ctx, command[0], command[1:], clipboardCommandOptions{input: text, timeoutMS: 5000}) != nil {
			copied = true
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	emit := func() (bool, error) {
		encoded := base64.StdEncoding.EncodeToString([]byte(text))
		if len(encoded) > MaxOSC52EncodedLength || write == nil {
			return false, nil
		}
		if err := write(ctx, "\x1b]52;c;"+encoded+"\x07"); err != nil {
			return false, err
		}
		return true, nil
	}

	osc52Emitted := false
	if !copied && goos == "linux" && IsWSL(getenv, readFile) {
		// WSL without WSLg writes through the Windows clipboard; Windows
		// Terminal prefers OSC 52 over the slower PowerShell round trip.
		if getenv("WT_SESSION") != "" {
			emitted, err := emit()
			if err != nil {
				return err
			}
			osc52Emitted = emitted
		}
		if osc52Emitted {
			copied = true
		} else {
			copied = copyViaWindowsClipboard(ctx, run, text)
		}
	}

	headless := goos == "linux" && getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" && getenv("TERMUX_VERSION") == ""
	oversized := false
	if !osc52Emitted && (isRemoteSessionFor(getenv) || (!copied && headless)) {
		emitted, err := emit()
		if err != nil {
			return err
		}
		if emitted {
			copied = true
		} else {
			oversized = true
		}
	}
	if copied {
		return nil
	}
	if oversized {
		return fmt.Errorf("Clipboard unavailable: text exceeds the OSC 52 size limit")
	}
	return clipboardUnavailableErrorFor(goos, getenv)
}

// copyViaWindowsClipboard writes text through PowerShell, reading it from a
// UTF-8 file so the console code page cannot mangle non-ASCII text (upstream
// copyViaWindowsClipboard).
func copyViaWindowsClipboard(ctx context.Context, run clipboardCommandRunner, text string) bool {
	tempFile := filepath.Join(os.TempDir(), AppName+"-wsl-clip-"+UUIDv7()+".txt")
	defer func() { _ = os.Remove(tempFile) }()
	if err := os.WriteFile(tempFile, []byte(text), 0o600); err != nil {
		return false
	}
	output := run(ctx, "wslpath", []string{"-w", tempFile}, clipboardCommandOptions{timeoutMS: 1000})
	if output == nil {
		return false
	}
	winPath := strings.TrimSpace(string(output))
	if winPath == "" {
		return false
	}
	script := "Set-Clipboard -Value ([System.IO.File]::ReadAllText('" + strings.ReplaceAll(winPath, "'", "''") + "', [System.Text.Encoding]::UTF8))"
	return run(ctx, "powershell.exe", []string{"-NoProfile", "-Command", script}, clipboardCommandOptions{timeoutMS: 5000}) != nil
}

// clipboardQueue serializes clipboard copies off the calling goroutine (the
// internal/offloop uniform mechanism). The clipboard subprocess can hang for
// up to its timeout — xclip serving a selection, a wedged clipboard daemon —
// so callers on the UI goroutine use this form and confirm optimistically;
// onDone runs on the worker and its caller marshals back onto the loop.
var clipboardQueue = offloop.New()

// CopyTextToClipboardAsync copies on the clipboard queue and reports the
// outcome to onDone (nil on success), in submission order.
func CopyTextToClipboardAsync(text string, onDone func(error)) {
	clipboardQueue.Go(func() {
		onDone(CopyTextToClipboard(text))
	})
}

// ReadClipboardText reads plain text from the system clipboard (upstream
// readClipboardText; the native clipboard fallback has no Go counterpart).
// Empty text returns ("", nil) like upstream's null.
func ReadClipboardText() (string, error) {
	return ReadClipboardTextContext(context.Background())
}

// ReadClipboardTextContext cancels platform clipboard readers with their mode.
func ReadClipboardTextContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, command := range pasteCommands() {
		if bytes := runClipboardCommand(ctx, command[0], command[1:], clipboardCommandOptions{timeoutMS: 5000}); bytes != nil {
			// Upstream decodes the command output with Buffer.toString("utf8").
			return DecodeUTF8WithReplacement(bytes), nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	return "", nil
}
