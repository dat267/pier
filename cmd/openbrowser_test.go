package cmd

import (
	"testing"

	"github.com/dat267/pier/coding/interactive"
)

// TestInstallBrowserOpener checks the CLI wires a browser opener, so the OAuth
// login flow opens the authorization URL (upstream utils/open-browser.ts).
func TestInstallBrowserOpener(t *testing.T) {
	previous := interactive.BrowserOpener()
	t.Cleanup(func() { interactive.SetBrowserOpener(previous) })
	interactive.SetBrowserOpener(nil)
	installBrowserOpener()
	if interactive.BrowserOpener() == nil {
		t.Fatal("the browser opener must be installed")
	}
}
