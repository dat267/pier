package interactive

import (
	"strings"
	"testing"
)

// pi v1.5.0 login-dialog.ts: pressing Enter on a device-code prompt opens the
// verification page and copies the code, once; a non-web URI is not opened.
func TestDeviceCodeConfirmOpensAndCopies(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)
	previousBrowser := browserOpener
	t.Cleanup(func() { browserOpener = previousBrowser })
	opened := ""
	browserOpener = func(url string) { opened = url }

	dialog := NewLoginDialogComponent(nil, nil, "github-copilot", nil, "", "")
	dialog.SetFocused(true)
	copied := ""
	copies := 0
	dialog.copyClipboard = func(text string, done func(error)) {
		copies++
		copied = text
		done(nil)
	}
	dialog.ShowDeviceCode("https://example.com/device", "ABCD-1234")
	dialog.HandleInput("\r")
	if opened != "https://example.com/device" {
		t.Fatalf("opened = %q", opened)
	}
	if copied != "ABCD-1234" || copies != 1 {
		t.Fatalf("copied = %q (%d)", copied, copies)
	}
	if lines := dialog.ContentLines(80); !strings.Contains(strings.Join(lines, "\n"), "Code copied to clipboard") {
		t.Fatalf("hint did not report the copy: %q", lines)
	}
	// Consumed once: a second Enter must not launch another browser or copy.
	dialog.HandleInput("\r")
	if copies != 1 {
		t.Fatalf("second Enter copied %d times", copies)
	}

	// A non-web verification URI is shown but never handed to the launcher.
	dialog2 := NewLoginDialogComponent(nil, nil, "github-copilot", nil, "", "")
	dialog2.SetFocused(true)
	dialog2.copyClipboard = func(string, func(error)) {}
	opened = ""
	dialog2.ShowDeviceCode("file:///tmp/evil", "CODE")
	dialog2.HandleInput("\r")
	if opened != "" {
		t.Fatalf("opened non-web URI %q", opened)
	}
}
