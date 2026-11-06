package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

func tailFingerprint(t *testing.T, manager *coding.SessionManager) string {
	t.Helper()
	fingerprint := ""
	for _, message := range manager.Projection().Messages {
		encoded, err := ai.MarshalJSON(message)
		if err != nil {
			t.Fatalf("marshal projected message: %v", err)
		}
		fingerprint += string(encoded) + "\n"
	}
	return fingerprint
}

// /tail is pier-only (D378): a new session seeded with the live context, so
// the compacted prefix stops traveling with the session while the projection
// stays identical. The command refuses sessions that do not persist, since there
// is no file to hand off from.
func TestTailCommandStartsFromTheLiveContext(t *testing.T) {
	app, cleanup := newTestAppPersisted(t)
	defer cleanup()

	if app.submit == nil {
		t.Fatal("the app has no submit wiring")
	}

	appendTurn(t, app, "cold question")
	app.sessionMgr.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "cold answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 10, Output: 5, TotalTokens: 15},
	})
	keptID := app.sessionMgr.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept question"}})
	app.sessionMgr.AppendCompaction("summary of the cold prefix", keptID, 15, nil, false, nil)
	appendTurn(t, app, "live question")

	beforeFile := app.sessionMgr.GetSessionFile()
	beforeID := app.sessionMgr.GetSessionID()
	beforeEntries := len(app.sessionMgr.GetEntries())
	beforeDigest := tailFingerprint(t, app.sessionMgr)
	if beforeFile == "" {
		t.Fatal("the persisted test session has no file")
	}

	app.submit.HandleSubmit(context.Background(), "/tail")
	if app.sessionMgr.GetSessionID() == beforeID {
		t.Error("tail kept the source session id")
	}
	if app.sessionMgr.GetSessionFile() == beforeFile {
		t.Error("tail kept the source session file")
	}
	if got := tailFingerprint(t, app.sessionMgr); got != beforeDigest {
		t.Errorf("tail context differs:\n got %q\nwant %q", got, beforeDigest)
	}
	if got := len(app.sessionMgr.GetEntries()); got >= beforeEntries {
		t.Errorf("tail kept %d of %d entries; the compacted prefix should be dropped", got, beforeEntries)
	}
	if texts := userMessageTexts(app); len(texts) != 2 || texts[0] != "kept question" || texts[1] != "live question" {
		t.Errorf("tail messages = %v, want only the live window", texts)
	}
	if !transcriptHasStatus(app, "Started a new session from the live context") {
		t.Errorf("no tail status: %q", transcriptTexts(app))
	}
	// The only way back to the source is /resume, so the status names it.
	if !transcriptHasStatus(app, "/resume") {
		t.Errorf("the tail status does not mention /resume: %q", transcriptTexts(app))
	}
}

func TestTailCommandNeedsAPersistedSession(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	appendTurn(t, app, "hello")
	beforeID := app.sessionMgr.GetSessionID()

	app.submit.HandleSubmit(context.Background(), "/tail")
	if app.sessionMgr.GetSessionID() != beforeID {
		t.Error("the refused tail still switched sessions")
	}
	if transcript := strings.Join(transcriptTexts(app), "\n"); !strings.Contains(transcript, "needs a saved session") {
		t.Errorf("no refusal error in the transcript: %q", transcript)
	}
}
