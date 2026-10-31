package interactive

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// pi 4a080252 components/session-selector.ts status timeouts run on the JS
// event loop. D143's Go selector must post timer mutations to its UI owner.
func TestSessionSelectorStatusTimerAppliesOnOwner(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	queue := &postQueue{}
	var fire func()
	loader := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
		return nil, errors.New("fixture offline")
	}
	component := NewSessionSelectorComponent(loader, loader, nil, nil, nil, nil,
		SessionSelectorOptions{Post: queue.Post, ScheduleTimer: func(_ int, fn func()) { fire = fn }})
	component.WaitForPendingLoads()
	queue.drain()
	statusShown := func() bool { return strings.Contains(strings.Join(component.Render(100), "\n"), "fixture offline") }
	if fire == nil || !statusShown() {
		t.Fatal("loader error did not publish a timed status")
	}
	done := make(chan struct{})
	go func() {
		fire()
		close(done)
	}()
	<-done
	if !statusShown() {
		t.Fatal("status timer mutated the selector before its owner drained the post")
	}
	queue.drain()
	if statusShown() {
		t.Fatal("status timer did not clear the status on its owner")
	}
}
