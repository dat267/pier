package coding

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCopyCommands pins the platform command selection (upstream
// copyToClipboard's command tables) without depending on the ambient
// environment: the DISPLAY/WAYLAND/TERMUX gates are read via os.Getenv, so
// the test only asserts ordering rules that hold regardless.
func TestClipboardOSC52OverrideAlsoCopiesAfterLocalSuccess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fake xclip command is Linux-specific")
	}
	binDir := t.TempDir()
	copiedFile := filepath.Join(t.TempDir(), "clipboard.txt")
	xclip := "#!/bin/sh\n/usr/bin/cat > \"$PIER_TEST_CLIPBOARD_FILE\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "xclip"), []byte(xclip), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PIER_TEST_CLIPBOARD_FILE", copiedFile)
	t.Setenv("DISPLAY", ":1")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("TERMUX_VERSION", "")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("MOSH_CONNECTION", "")
	t.Setenv("PI_CLIPBOARD_OSC52", "1")

	var packets []string
	err := CopyTextToClipboardWithOSC52(context.Background(), "hello", func(_ context.Context, packet string) error {
		packets = append(packets, packet)
		return nil
	})
	if err != nil {
		t.Fatalf("copy error: %v", err)
	}
	if copied, err := os.ReadFile(copiedFile); err != nil || string(copied) != "hello" {
		t.Fatalf("local clipboard = %q, err = %v", copied, err)
	}
	wantPacket := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello")) + "\x07"
	if len(packets) != 1 || packets[0] != wantPacket {
		t.Fatalf("OSC 52 packets = %q, want %q", packets, wantPacket)
	}
}

func TestClipboardCommandsShape(t *testing.T) {
	commands := copyCommands()
	switch {
	case len(commands) == 0:
		t.Skip("no clipboard writers available in this environment")
	case isRemoteSession():
		// OSC 52 fallback covers remote sessions; command presence is
		// environment-dependent.
	}
	// Every command is a name plus optional flags; no command is empty.
	for _, command := range commands {
		if len(command) == 0 || command[0] == "" {
			t.Fatalf("invalid command %v", command)
		}
	}
}

// TestClipboardUnavailableMessage pins the upstream error text for an
// environment with no display (forced by clearing the env gates via the
// message table rather than mutating the process environment).
func TestClipboardUnavailableMessage(t *testing.T) {
	// The message table is asserted directly: a headless Linux session now has
	// the OSC 52 fallback, so the entry point no longer fails there.
	empty := func(string) string { return "" }
	if err := clipboardUnavailableErrorFor("linux", empty); !strings.Contains(err.Error(), "no Wayland or X11 display") {
		t.Fatalf("headless message = %v", err)
	}
	display := func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	if err := clipboardUnavailableErrorFor("linux", display); !strings.Contains(err.Error(), "install `xclip` or `xsel`") {
		t.Fatalf("x11 message = %v", err)
	}
	if err := clipboardUnavailableErrorFor("windows", empty); err.Error() != "Clipboard unavailable" {
		t.Fatalf("windows message = %v", err)
	}
}

// TestClipboardTermuxPasteOnAndroid pins upstream #10391: Termux reports
// platform "android", not "linux", so the read path must select
// termux-clipboard-get regardless of platform and the failure hint must name
// Termux. copyCommands already had no platform gate, so only read was broken.
func TestClipboardTermuxPasteOnAndroid(t *testing.T) {
	env := func(key string) string {
		if key == "TERMUX_VERSION" {
			return "0.118.0"
		}
		return ""
	}
	commands := pasteCommandsFor("android", env)
	if len(commands) != 1 || commands[0][0] != "termux-clipboard-get" {
		t.Fatalf("android paste commands = %v, want termux-clipboard-get", commands)
	}
	err := clipboardUnavailableErrorFor("android", env)
	if err == nil || !strings.Contains(err.Error(), "Termux:API") {
		t.Fatalf("android clipboard error = %v, want the Termux hint", err)
	}
}

// Linux keeps the X11/Wayland readers in upstream order.
func TestClipboardLinuxPasteCommands(t *testing.T) {
	env := func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	commands := pasteCommandsFor("linux", env)
	if len(commands) != 2 || commands[0][0] != "xclip" || commands[1][0] != "xsel" {
		t.Fatalf("linux paste commands = %v", commands)
	}
}

// A Termux copy on android takes the env-based branch (copyCommands has no
// platform gate), which the pre-fix code already did.
func TestClipboardTermuxCopyOnAndroid(t *testing.T) {
	env := func(key string) string {
		if key == "TERMUX_VERSION" {
			return "0.118.0"
		}
		return ""
	}
	commands := copyCommandsFor("android", env)
	if len(commands) != 1 || commands[0][0] != "termux-clipboard-set" {
		t.Fatalf("android copy commands = %v", commands)
	}
}

func plainLinuxFile(string) ([]byte, error) { return []byte("Linux version"), nil }

// pi v1.4.0 copyToClipboard: WSL prefers OSC 52 under Windows Terminal over
// the PowerShell round trip.
func TestCopyTextWSLPrefersOSC52UnderWindowsTerminal(t *testing.T) {
	env := map[string]string{"WSL_DISTRO_NAME": "Ubuntu", "WT_SESSION": "session"}
	run := func(context.Context, string, []string, clipboardCommandOptions) []byte {
		t.Fatal("no clipboard command expected when OSC 52 is available")
		return nil
	}
	var packets []string
	err := copyTextToClipboardWith(context.Background(), "linux", func(key string) string { return env[key] },
		run, plainLinuxFile, func(_ context.Context, packet string) error {
			packets = append(packets, packet)
			return nil
		}, "hello")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(packets) != 1 || !strings.Contains(packets[0], "aGVsbG8=") {
		t.Fatalf("packets = %q", packets)
	}
}

// WSL without WT_SESSION writes through PowerShell (wslpath + Set-Clipboard).
func TestCopyTextWSLUsesPowerShellFallback(t *testing.T) {
	env := map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}
	var commands []string
	run := func(_ context.Context, command string, args []string, _ clipboardCommandOptions) []byte {
		commands = append(commands, command)
		switch command {
		case "wslpath":
			return []byte(`C:\Temp\clip.txt`)
		case "powershell.exe":
			if strings.Contains(strings.Join(args, " "), "Set-Clipboard") {
				return []byte{}
			}
		}
		return nil
	}
	packets := 0
	err := copyTextToClipboardWith(context.Background(), "linux", func(key string) string { return env[key] },
		run, plainLinuxFile, func(context.Context, string) error { packets++; return nil }, "hello")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(commands) != 2 || commands[0] != "wslpath" || commands[1] != "powershell.exe" {
		t.Fatalf("commands = %v", commands)
	}
	if packets != 0 {
		t.Fatalf("OSC 52 emitted after a successful PowerShell copy: %d", packets)
	}
}

// A headless Linux session without a display still falls back to OSC 52.
func TestCopyTextHeadlessUsesOSC52(t *testing.T) {
	run := func(context.Context, string, []string, clipboardCommandOptions) []byte { return nil }
	var packets []string
	err := copyTextToClipboardWith(context.Background(), "linux", func(string) string { return "" },
		run, plainLinuxFile, func(_ context.Context, packet string) error {
			packets = append(packets, packet)
			return nil
		}, "hello")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("packets = %q", packets)
	}
}

func TestCopyTextHeadlessOversizedOSC52Fails(t *testing.T) {
	run := func(context.Context, string, []string, clipboardCommandOptions) []byte { return nil }
	err := copyTextToClipboardWith(context.Background(), "linux", func(string) string { return "" },
		run, plainLinuxFile, func(context.Context, string) error { t.Fatal("packet written"); return nil },
		strings.Repeat("a", 100_001))
	if err == nil || !strings.Contains(err.Error(), "exceeds the OSC 52 size limit") {
		t.Fatalf("err = %v", err)
	}
}

// A non-headless Linux session with no writer reports the X11 hint.
func TestCopyTextUnavailableOnDesktop(t *testing.T) {
	env := map[string]string{"DISPLAY": ":0"}
	run := func(context.Context, string, []string, clipboardCommandOptions) []byte { return nil }
	err := copyTextToClipboardWith(context.Background(), "linux", func(key string) string { return env[key] },
		run, plainLinuxFile, func(context.Context, string) error { return nil }, "hello")
	if err == nil || !strings.Contains(err.Error(), "install `xclip` or `xsel`") {
		t.Fatalf("err = %v", err)
	}
}

// pi v1.4.0 clipboard-command.ts aborts a read whose output exceeds
// maxBufferBytes (50 MiB by default) instead of collecting it. An oversized
// clipboard must fail the reader, not be buffered in full.
func TestClipboardReadAbortsOverMaxBuffer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh fixture")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	oversized := runClipboardCommand(context.Background(), sh,
		[]string{"-c", "printf '%064d' 0"},
		clipboardCommandOptions{timeoutMS: 5000, maxBufferBytes: 32})
	if oversized != nil {
		t.Fatalf("oversized read returned %d bytes, want the abort", len(oversized))
	}
	fits := runClipboardCommand(context.Background(), sh,
		[]string{"-c", "printf '%064d' 0"},
		clipboardCommandOptions{timeoutMS: 5000, maxBufferBytes: 64})
	if len(fits) != 64 {
		t.Fatalf("within-limit read = %d bytes, want 64", len(fits))
	}
}
