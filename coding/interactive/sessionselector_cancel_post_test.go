package interactive

import (
	"context"
	"testing"

	"github.com/dat267/pier/coding"
)

// pi 4a080252 components/session-selector.ts loadScope checks isActive before
// applying progress or completion. D143 moves those applies onto the Go UI
// queue, so cancellation must also be checked when a posted callback executes.
func TestSessionSelectorRejectsPostedResultsAfterCancel(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	for _, cancel := range []bool{true, false} {
		name := "canceled"
		if !cancel {
			name = "active-control"
		}
		t.Run(name, func(t *testing.T) {
			queue := &postQueue{}
			renders, cancels := 0, 0
			loader := func(progress SessionListProgress, _ context.Context) ([]coding.SessionInfo, error) {
				sessions := []coding.SessionInfo{{ID: "old", Path: "/fixture/old.jsonl", FirstMessage: "old"}}
				progress(1, 2, sessions)
				return sessions, nil
			}
			component := NewSessionSelectorComponent(loader, loader, nil,
				func() { cancels++ }, nil, func() { renders++ },
				SessionSelectorOptions{Post: queue.Post, ScheduleTimer: func(int, func()) {}})
			component.WaitForPendingLoads()
			if cancel {
				component.HandleInput("\x1b")
				if cancels != 1 {
					t.Fatal("cancel input did not close the selector")
				}
			}
			before := renders
			queue.drain()
			selected := component.GetSessionList().GetSelectedSessionPath()
			if cancel {
				if selected != "" || renders != before {
					t.Fatalf("canceled load applied queued results: selected=%q, extra renders=%d", selected, renders-before)
				}
			} else if selected != "/fixture/old.jsonl" || renders == before {
				t.Fatalf("active load did not apply: selected=%q, extra renders=%d", selected, renders-before)
			}
		})
	}
}
