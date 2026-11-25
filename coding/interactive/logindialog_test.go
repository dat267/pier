package interactive

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
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

// D460: an authorization server's text is data, not terminal control. The
// reference prints the instructions, the device user code and an info message
// as they arrive; pier sanitizes remote text (D342).
func TestLoginDialogDropsProviderTerminalControl(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	component := NewLoginDialogComponent(nil, nil, "xai", nil, "", "")
	render := func() string { return strings.Join(component.Render(120), "\n") }
	expectClean := func(stage string, wants ...string) {
		t.Helper()
		rendered := render()
		for _, injected := range []string{"]0;owned", "]8;;https://evil", "\x1b[2J", "\x1b[31m"} {
			if strings.Contains(rendered, injected) {
				t.Fatalf("%s: provider control %q reached the dialog: %q", stage, injected, rendered)
			}
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s: dialog dropped %q: %q", stage, want, rendered)
			}
		}
	}

	// ShowAuth replaces the body, so each stage renders on its own.
	component.ShowAuth("https://x.ai/authorize", "line one\n\x1b[2Jline two")
	expectClean("auth", "line one", "line two")

	component.ShowDeviceCode("https://x.ai/device", "\x1b]0;owned\x07ABCD-1234")
	expectClean("device", "ABCD-1234")

	component.ShowInfo("\x1b]8;;https://evil\x07click", []ai.AuthInfoLink{{URL: "https://x.ai/help", Label: "\x1b[31mhelp\x1b[0m"}}, false)
	expectClean("info", "click", "help")
}

type authStatusPostCapture struct {
	tui.TUI
	posts chan func()
}

func (c *authStatusPostCapture) Post(fn func()) { c.posts <- fn }

// pi v1.4.0 (4a080252), interactive-mode.ts login status and
// program-status-reporter.ts run on the JS owner. D156 moves only the waiting
// flow to a worker: its final status must wait for the same owner post as the
// editor restoration, not mutate ProgramStatusReporter on that worker.
func TestLoginCompletionStatusWaitsForOwnerPost(t *testing.T) {
	wiring, session := newAuthTestWiring(t)
	capture := &authStatusPostCapture{TUI: wiring.UI, posts: make(chan func(), 1)}
	wiring.UI = capture
	reporter, statuses := newProgramStatusReport(t, "fixture")
	wiring.ProgramStatus = reporter
	entered, release := make(chan struct{}), make(chan struct{})
	provider := session.runtime.GetProvider("openai")
	provider.Auth.APIKey.Login = func(*ai.AuthInteraction) (*ai.ApiKeyCredential, error) {
		close(entered)
		<-release
		return nil, errors.New("synthetic login failure")
	}
	wiring.ShowApiKeyLoginDialog(context.Background(), "openai", "OpenAI")
	<-entered
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusBlocked {
		close(release)
		t.Fatalf("login did not report blocked: %+v", got)
	}
	close(release)
	var apply func()
	select {
	case apply = <-capture.posts:
	case <-time.After(time.Second):
		t.Fatal("login did not post completion")
	}
	before := lastStatus(t, statuses())
	apply()
	if before.State != tui.ProgramStatusBlocked {
		t.Errorf("worker cleared attached program status before owner post: %+v", before)
	}
	if got := lastStatus(t, statuses()); got.State != tui.ProgramStatusIdle {
		t.Errorf("owner completion did not clear blocked status: %+v", got)
	}
}

// TestLoginDialogDeviceCodeAfterCancelIgnored covers the aborted guard. The
// reference's cancel clears the device-code action and handleInput checks
// `!this.signal.aborted`, so Enter after a cancel does nothing. Without it a
// cancelled login still copied the one-time code and opened the browser.
func TestLoginDialogDeviceCodeAfterCancelIgnored(t *testing.T) {
	InitTheme("dark", false)
	dialog := NewLoginDialogComponent(nil, nil, "p", nil, "", "")
	dialog.SetFocused(true)
	copies, opens := 0, 0
	dialog.copyClipboard = func(string, func(error)) { copies++ }
	SetBrowserOpener(func(string) { opens++ })
	defer SetBrowserOpener(func(string) {})

	dialog.ShowDeviceCode("https://example.com/device", "CODE")
	dialog.HandleInput("\x1b") // cancel
	dialog.HandleInput("\r")   // Enter after the cancel
	if copies != 0 || opens != 0 {
		t.Fatalf("after cancel copied %d and opened %d, want neither", copies, opens)
	}

	// The same sequence without a cancel still acts.
	live := NewLoginDialogComponent(nil, nil, "p", nil, "", "")
	live.SetFocused(true)
	live.copyClipboard = func(string, func(error)) { copies++ }
	live.ShowDeviceCode("https://example.com/device", "CODE")
	live.HandleInput("\r")
	if copies != 1 || opens != 1 {
		t.Fatalf("before cancel copied %d and opened %d, want one of each", copies, opens)
	}
}

// TestLoginDialogClearsTheDeviceCodeHintRow covers clearDeviceCodeAction. The
// reference removes the hint child as well as the action, so a later prompt does
// not leave the device-code hint on screen, where its Enter hint is already dead.
func TestLoginDialogClearsTheDeviceCodeHintRow(t *testing.T) {
	InitTheme("dark", false)
	dialog := NewLoginDialogComponent(nil, nil, "p", nil, "", "")
	dialog.SetFocused(true)
	dialog.copyClipboard = func(string, func(error)) {}

	dialog.ShowDeviceCode("https://example.com/device", "CODE")
	if lines := strings.Join(dialog.ContentLines(80), "\n"); !strings.Contains(lines, "to open browser and copy code") {
		t.Fatalf("device code hint missing while the action is live: %q", lines)
	}
	dialog.ShowPrompt("Paste code", "")
	if lines := strings.Join(dialog.ContentLines(80), "\n"); strings.Contains(lines, "to open browser and copy code") {
		t.Fatalf("stale device code hint survived the next prompt: %q", lines)
	}
	if lines := strings.Join(dialog.ContentLines(80), "\n"); !strings.Contains(lines, "Paste code") {
		t.Fatalf("the prompt itself is missing: %q", lines)
	}
}

// The opener is read from the login flow's goroutine, not the owner loop: opening
// a browser runs exec, which can block, so it must not run on the loop. The seam
// is exported and a test or embedder installs it while a dialog may be running,
// so the read and the write need the same lock.
func TestBrowserOpenerIsSafeForConcurrentUse(t *testing.T) {
	previous := BrowserOpener()
	defer SetBrowserOpener(previous)

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for range 50 {
			SetBrowserOpener(func(string) {})
		}
	}()
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for range 50 {
			BrowserOpener()("https://example.test")
		}
	}()
	waitGroup.Wait()
}
