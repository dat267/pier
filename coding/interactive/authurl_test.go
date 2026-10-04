package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// TestAuthUrlComponentCopy covers the sign-in URL block: the URL and copy hint
// render, Copy copies the URL, and the hint reports success.
func TestAuthUrlComponentCopy(t *testing.T) {
	InitTheme("dark", false)
	previous := authURLCopier
	t.Cleanup(func() { authURLCopier = previous })
	var copied string
	authURLCopier = func(text string, onDone func(error)) {
		copied = text
		onDone(nil)
	}

	component := NewAuthUrlComponent(nil, nil, "https://example.com/auth")
	rendered := strings.Join(component.Render(80), "\n")
	if !strings.Contains(rendered, "https://example.com/auth") || !strings.Contains(rendered, "to copy") {
		t.Fatalf("render = %q", rendered)
	}
	component.Copy()
	if copied != "https://example.com/auth" {
		t.Fatalf("copied = %q", copied)
	}
	if after := strings.Join(component.Render(80), "\n"); !strings.Contains(after, "Copied URL to clipboard") {
		t.Fatalf("hint = %q", after)
	}
}

// TestLoginDialogCopiesAuthURL checks app.message.copy routes to the dialog's
// sign-in URL block and that a later step clears it.
func TestLoginDialogCopiesAuthURL(t *testing.T) {
	InitTheme("dark", false)
	appKeybindings := NewAppKeybindingsManager(nil, "")
	previousBindings := tui.GetKeybindings()
	tui.SetKeybindings(appKeybindings.KeybindingsManager)
	t.Cleanup(func() { tui.SetKeybindings(previousBindings) })
	previous := authURLCopier
	t.Cleanup(func() { authURLCopier = previous })
	var copied string
	authURLCopier = func(text string, onDone func(error)) {
		copied = text
		onDone(nil)
	}

	dialog := NewLoginDialogComponent(nil, nil, "openai", nil, "", "")
	dialog.ShowAuth("https://example.com/auth", "")
	if dialog.authUrl == nil {
		t.Fatal("showAuth must mount the URL block")
	}
	// app.message.copy defaults to ctrl+x.
	dialog.HandleInput("\x18")
	if copied != "https://example.com/auth" {
		t.Fatalf("copied = %q", copied)
	}
	// A later step unmounts it, so the key goes back to the input.
	dialog.ShowDetails([]string{"details"})
	if dialog.authUrl != nil {
		t.Fatal("showDetails must clear the URL block")
	}
}
