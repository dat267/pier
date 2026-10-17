package coding

import (
	"context"
	"encoding/base64"
	"os"
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
	// The premise is an environment with no clipboard backend. Windows and macOS
	// ship one (copyCommands returns clip.exe / pbcopy), so only Linux can have a
	// backendless clipboard.
	if runtime.GOOS != "linux" {
		t.Skip("the platform ships a clipboard command (clip.exe / pbcopy)")
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != "" || os.Getenv("TERMUX_VERSION") != "" {
		t.Skip("clipboard helpers present")
	}
	if isRemoteSession() {
		t.Skip("remote session: OSC 52 fallback copies without a local backend")
	}
	err := CopyTextToClipboard("probe")
	if err == nil {
		t.Fatal("copy succeeded without any clipboard backend")
	}
	if !strings.Contains(err.Error(), "Clipboard unavailable") {
		t.Fatalf("err = %v", err)
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
