package interactive

import (
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) components/login-dialog.ts showDetails calls
// clearDeviceCodeAction() before replacing the content, so the Enter action of a
// device-code panel cannot fire once the panel is gone. Pier's ShowDetails left
// the action in place: after a device code, opening details and pressing Enter
// still copied the code and opened the browser.
func TestLoginDialogDetailsClearsTheDeviceCodeAction(t *testing.T) {
	InitTheme("dark", false)
	opened := 0
	previousOpener := BrowserOpener()
	SetBrowserOpener(func(string) { opened++ })
	defer SetBrowserOpener(previousOpener)

	copies := 0
	component := NewLoginDialogComponent(nil, nil, "github-copilot", nil, "", "")
	component.SetFocused(true)
	component.copyClipboard = func(string, func(error)) { copies++ }
	component.ShowDeviceCode("https://github.com/login/device", "CODE-1")
	component.ShowDetails([]string{"Provider information"})

	component.HandleInput("\r")
	if opened != 0 || copies != 0 {
		t.Fatalf("a stale device-code action ran: opened %d copies %d", opened, copies)
	}

	// The action still runs while its panel is shown.
	component.ShowDeviceCode("https://github.com/login/device", "CODE-2")
	component.HandleInput("\r")
	if opened != 1 || copies != 1 {
		t.Fatalf("the shown device-code action did not run: opened %d copies %d", opened, copies)
	}

	// A second Enter is consumed.
	component.HandleInput("\r")
	if opened != 1 || copies != 1 {
		t.Fatalf("the action ran twice: opened %d copies %d", opened, copies)
	}
}
