package interactive

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// TestSessionReplacementDoesNotRebuildTheProjectionOnTheLoop pins A494: the
// worker that opens a session also derives its projection, so the posted rebind
// does not decode every entry on the owner loop. At 12,500 turns that decode was
// 151 ms of a 204 ms rebind.
//
// The check is a count, not a clock: ProjectionBuilds only moves when an
// uncached projection is derived, so any build during the rebind is one the loop
// paid.
func TestSessionReplacementDoesNotRebuildTheProjectionOnTheLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	cwd := t.TempDir()

	session := coding.NewSessionManager(cwd, nil)
	for turn := 0; turn < 2000; turn++ {
		session.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
		session.AppendMessage(&ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
			Content:    ai.ContentList{ai.TextContent{Text: "answer"}},
			StopReason: ai.StopStop,
		})
	}
	path := session.GetSessionFile()

	opened, err := app.openSessionWork(path, "")(context.Background())
	if err != nil {
		t.Fatalf("openSessionWork: %v", err)
	}
	before := opened.ProjectionBuilds()
	if _, err := app.applyOpenedSession(opened, nil); err != nil {
		t.Fatalf("applyOpenedSession: %v", err)
	}
	if after := opened.ProjectionBuilds(); after != before {
		t.Fatalf("the rebind rebuilt the projection %d time(s) on the loop; the worker must warm it", after-before)
	}
}
