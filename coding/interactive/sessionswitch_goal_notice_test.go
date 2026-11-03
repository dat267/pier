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

// D223/D196: a callback from a retired controller must not reach the new
// transcript. Reference: pi v1.5.0 (f9a26365) interactive-mode.ts binds UI
// callbacks to the current runtime; Go's queued owner handoff can outlive it.
func TestSessionReplacementRejectsRetiredGoalNotices(t *testing.T) {
	for _, when := range []string{"already posted", "delivered late"} {
		t.Run(when, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.Init(context.Background())
			old := app.session.Goal
			if old == nil {
				t.Fatal("goal controller is missing")
			}
			if when == "already posted" {
				old.HandleCommand("resume")
			}
			if _, err := app.sessionNew(context.Background()); err != nil {
				t.Fatal(err)
			}
			if when == "delivered late" {
				old.HandleCommand("resume")
			}
			app.ui.RenderNow(true)
			if chat := renderAppChat(app); strings.Contains(chat, "No stopped goal to resume.") {
				t.Fatalf("retired controller warning reached the new session: %q", chat)
			}
			app.submit.HandleSubmit(context.Background(), "/goal status")
			app.ui.RenderNow(true)
			if chat := renderAppChat(app); !strings.Contains(chat, "No goal set. Use /goal set <objective>") {
				t.Fatalf("current controller notice was dropped: %q", chat)
			}
		})
	}
}
