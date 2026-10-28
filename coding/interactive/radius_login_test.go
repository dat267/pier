package interactive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// pi v1.4.0 (4a080252), components/radius-login-selector.ts animates
// only a selected Radius row whose first rendered line matches the base selector,
// and stops when the menu is disposed.
func TestRadiusLoginMenuShimmersOnlyWhileSelected(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	synctest.Test(t, func(t *testing.T) {
		wiring.HandleLoginCommand(context.Background(), "")
		component := wiring.EditorContainer.Children[0]
		animator, ok := component.(tui.Animator)
		if !ok {
			t.Fatal("Radius login menu has no animation")
		}
		component.Render(100)
		if active, _ := animator.AnimationFrame(time.Now()); active {
			t.Fatal("unselected Radius row must not animate")
		}
		input := component.(interface{ HandleInput(string) })
		input.HandleInput("j")
		input.HandleInput("j")
		first := strings.Join(component.Render(100), "\n")
		if !strings.Contains(first, "\x1b[38;2;77;154;191mS") {
			t.Fatalf("first Radius shimmer color missing: %q", first)
		}
		if active, delay := animator.AnimationFrame(time.Now()); !active || delay != 50*time.Millisecond {
			t.Fatalf("animation = %v, %v", active, delay)
		}
		time.Sleep(400 * time.Millisecond)
		next := strings.Join(component.Render(100), "\n")
		if !strings.Contains(next, "\x1b[38;2;240;144;130mS") || next == first {
			t.Fatalf("Radius shimmer did not advance: %q", next)
		}
		if tui.StripTerminalSequences(first) != tui.StripTerminalSequences(next) {
			t.Fatal("animation changed menu text")
		}
		// Despite the upstream comment about wrapping, v1.4.0 compares only
		// the first rendered line: the selected row still animates at width 15.
		component.Render(15)
		if active, _ := animator.AnimationFrame(time.Now()); !active {
			t.Fatal("wrapped Radius row must retain upstream's first-line match")
		}
		wiring.Slot.DisposeActiveSelector()
		if active, _ := animator.AnimationFrame(time.Now()); active {
			t.Fatal("disposed Radius menu still animates")
		}
	})
}

// pi v1.4.0 (4a080252), interactive-mode.ts showLoginDialog and
// offerRadiusMcpServer ask before creating the global provider-auth entry.
func TestRadiusLoginOffersMcpConfiguration(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	provider := app.session.ModelRuntime().GetProvider("radius")
	provider.RefreshModels = func(*ai.RefreshModelsContext) error { return nil }
	provider.Auth.OAuth.Login = func(*ai.AuthInteraction) (*ai.OAuthCredential, error) {
		return &ai.OAuthCredential{OAuthCredentials: ai.OAuthCredentials{Access: "test-token", Expires: time.Now().Add(time.Hour).UnixMilli()}}, nil
	}
	app.commands.ReloadNow = func() (string, bool, error) { return "", false, nil }
	app.commands.ReloadMCP = nil // Never connect to the hosted service in a test.
	app.auth.ShowLoginDialog(context.Background(), "radius", "Radius")
	waitForCondition(t, func() bool {
		app.ui.RenderNow(true)
		return strings.Contains(strings.Join(app.editorContainer.Render(160), "\n"), "Configure Radius MCP in ")
	})
	mcpPath := filepath.Join(app.mcpAgentDir, "mcp.json")
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatalf("configuration written before consent: %v", err)
	}
	app.editorContainer.Children[0].(interface{ HandleInput(string) }).HandleInput("\r")
	app.offloopGroup.FlushAll()
	app.ui.RenderNow(true)
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"mcpServers\": {\n    \"radius\": {\n      \"url\": \"https://radius.pi.dev/mcp\",\n      \"auth\": {\n        \"provider\": \"radius\"\n      }\n    }\n  }\n}\n"
	if string(data) != want {
		t.Fatalf("MCP config = %s, want %s", data, want)
	}
}

// pi v1.4.0 (4a080252), interactive-mode.ts showAuthSelect gives Radius
// a description, maps labels back to ids, and restores the login on dismissal.
func TestRadiusLoginSelectIntro(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "select", true: "cancel"}[cancel], func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			answers := make(chan LoginInputResult, 1)
			app.session.ModelRuntime().GetProvider("radius").Auth.OAuth.Login = func(interaction *ai.AuthInteraction) (*ai.OAuthCredential, error) {
				value, err := interaction.Prompt(ai.AuthPrompt{
					Type: ai.AuthPromptSelect, Message: "Choose Radius login:",
					SelectOptions: []ai.AuthSelectOption{{ID: "browser", Label: "Browser"}, {ID: "device", Label: "Device"}},
				})
				answers <- LoginInputResult{Value: value, Err: err}
				return nil, errors.New("Login cancelled")
			}
			app.auth.ShowLoginDialog(context.Background(), "radius", "Radius")
			waitForCondition(t, func() bool {
				app.ui.RenderNow(true)
				return strings.Contains(strings.Join(app.editorContainer.Render(120), "\n"), "Choose Radius login:")
			})
			text := tui.StripTerminalSequences(strings.Join(app.editorContainer.Render(120), "\n"))
			if !strings.Contains(text, "Radius is a service crafted for Pi by the builders of Pi, Earendil Works") {
				// Always let the off-loop login finish, including on assertion failure.
				app.editorContainer.Children[0].(interface{ HandleInput(string) }).HandleInput("\x1b")
				t.Errorf("Radius intro missing: %s", text)
			} else {
				selector := app.editorContainer.Children[0].(interface{ HandleInput(string) })
				if cancel {
					selector.HandleInput("\x1b")
				} else {
					selector.HandleInput("j")
					selector.HandleInput("\r")
				}
			}
			var answer LoginInputResult
			waitForCondition(t, func() bool {
				app.ui.RenderNow(true)
				select {
				case answer = <-answers:
					return true
				default:
					return false
				}
			})
			if !t.Failed() && ((!cancel && (answer.Value != "device" || answer.Err != nil)) || (cancel && answer.Err == nil)) {
				t.Fatalf("answer = %+v, cancel = %v", answer, cancel)
			}
			waitForCondition(t, func() bool {
				app.ui.RenderNow(true)
				return findLoginDialog(app.auth) == nil
			})
		})
	}
}

// pi v1.4.0 (4a080252), interactive-mode.ts showLoginAuthTypeSelector
// offers Radius last and returns to that menu when its OAuth login is cancelled.
func TestRadiusLoginMenuShortcut(t *testing.T) {
	wiring, session := newAuthTestWiring(t)
	session.runtime.GetProvider("radius").Auth.OAuth.Login = func(interaction *ai.AuthInteraction) (*ai.OAuthCredential, error) {
		_, err := interaction.Prompt(ai.AuthPrompt{Type: ai.AuthPromptText, Message: "Radius test login"})
		return nil, err
	}
	wiring.HandleLoginCommand(context.Background(), "")
	before := tui.StripTerminalSequences(strings.Join(wiring.EditorContainer.Render(100), "\n"))
	if !strings.Contains(before, "Sign in with Radius • not configured") {
		t.Fatalf("Radius shortcut missing from login menu: %s", before)
	}
	selector := wiring.EditorContainer.Children[0].(interface{ HandleInput(string) })
	selector.HandleInput("j")
	selector.HandleInput("j")
	selector.HandleInput("\r")
	screen := wiring.UI.(*tui.MainScreen)
	waitForCondition(t, func() bool {
		screen.RenderNow(true)
		return strings.Contains(strings.Join(wiring.EditorContainer.Render(100), "\n"), "Radius test login")
	})
	findLoginDialog(wiring).HandleInput("\x1b")
	waitForCondition(t, func() bool {
		screen.RenderNow(true)
		return findLoginDialog(wiring) == nil
	})
	if got := tui.StripTerminalSequences(strings.Join(wiring.EditorContainer.Render(100), "\n")); got != before {
		t.Fatalf("cancel did not restore login menu:\ngot %q\nwant %q", got, before)
	}
}
