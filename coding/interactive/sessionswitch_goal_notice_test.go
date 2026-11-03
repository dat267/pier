package interactive

import (
	"context"
	"strings"
	"testing"
)

// D223's native goal notices must follow the current controller on every bind.
// Stock pi v1.5.0 (f9a26365) has no goal feature, but its coding-agent/src/modes/
// interactive/interactive-mode.ts rebindCurrentSession reconnects UI callbacks.
func TestSessionReplacementKeepsGoalCommandNoticesVisible(t *testing.T) {
	for _, kind := range []string{"initial", "new", "switch"} {
		t.Run(kind, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.Init(context.Background())
			switch kind {
			case "new":
				if _, err := app.sessionNew(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "switch":
				target := makePersistedSession(t, t.TempDir(), "another session")
				if _, err := app.switchSession(context.Background(), target.GetSessionFile(), ""); err != nil {
					t.Fatal(err)
				}
			}
			if app.session.Goal == nil {
				t.Fatal("goal controller is missing")
			}
			app.submit.HandleSubmit(context.Background(), "/goal status")
			app.ui.RenderNow(true)
			if chat := renderAppChat(app); !strings.Contains(chat, "No goal set. Use /goal set <objective>") {
				t.Fatalf("goal status notice missing from %s session: %q", kind, chat)
			}
		})
	}
}
