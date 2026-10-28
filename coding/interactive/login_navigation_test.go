package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// pi v1.4.0 (4a080252), interactive-mode.ts showLoginAuthTypeSelector
// reopens the provider-specific method menu when its login is cancelled.
func TestLoginCancelReturnsToAuthMethodSelector(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	wiring.HandleLoginCommand(context.Background(), "anthropic")
	before := strings.Join(wiring.EditorContainer.Render(100), "\n")
	selector := wiring.EditorContainer.Children[0].(*ExtensionSelectorComponent)
	selector.HandleInput("j")
	selector.HandleInput("\r")
	screen := wiring.UI.(*tui.MainScreen)
	waitForCondition(t, func() bool {
		screen.RenderNow(true)
		dialog := findLoginDialog(wiring)
		return dialog != nil && dialog.pending != nil
	})
	findLoginDialog(wiring).HandleInput("\x1b")
	waitForCondition(t, func() bool {
		screen.RenderNow(true)
		return findLoginDialog(wiring) == nil
	})
	if got := strings.Join(wiring.EditorContainer.Render(100), "\n"); got != before {
		t.Fatalf("cancel did not restore authentication methods:\ngot %q\nwant %q", got, before)
	}
}

// pi v1.4.0 (4a080252), interactive-mode.ts showLoginProviderSelector
// passes an onBack continuation through OAuth, API-key and ambient dialogs.
func TestLoginCancelReturnsToProviderSelector(t *testing.T) {
	for _, method := range []string{"oauth", "api_key", "ambient"} {
		t.Run(method, func(t *testing.T) {
			wiring, session := newAuthTestWiring(t)
			provider := session.runtime.GetProvider("anthropic")
			provider.Auth.OAuth.Login = func(interaction *ai.AuthInteraction) (*ai.OAuthCredential, error) {
				_, err := interaction.Prompt(ai.AuthPrompt{Type: ai.AuthPromptText, Message: "Test login"})
				return nil, err
			}
			provider.Auth.APIKey.Login = func(interaction *ai.AuthInteraction) (*ai.ApiKeyCredential, error) {
				_, err := interaction.Prompt(ai.AuthPrompt{Type: ai.AuthPromptText, Message: "Test login"})
				return nil, err
			}
			authType := method
			if method == "ambient" {
				provider.Auth.APIKey.Login = nil
				authType = "api_key"
			}
			wiring.ShowLoginProviderSelector(context.Background(), authType, "anthropic")
			before := strings.Join(wiring.EditorContainer.Render(100), "\n")
			selector := wiring.EditorContainer.Children[0].(*OAuthSelectorComponent)
			selector.HandleInput("\r")
			screen := wiring.UI.(*tui.MainScreen)
			if method != "ambient" {
				waitForCondition(t, func() bool {
					screen.RenderNow(true)
					return strings.Contains(strings.Join(wiring.EditorContainer.Render(100), "\n"), "Test login")
				})
			}
			dialog := findLoginDialog(wiring)
			if dialog == nil {
				t.Fatal("login dialog not mounted")
			}
			dialog.HandleInput("\x1b")
			waitForCondition(t, func() bool {
				screen.RenderNow(true)
				return findLoginDialog(wiring) == nil
			})
			if got := strings.Join(wiring.EditorContainer.Render(100), "\n"); got != before {
				t.Fatalf("cancel did not restore the provider list and initial search:\ngot %q\nwant %q", got, before)
			}
		})
	}
}
