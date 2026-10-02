package interactive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// A session replacement resets the view (upstream rebindCurrentSession calls
// renderCurrentSessionState: clear the loaded-resources container, the chat, the
// pending-message container, the streaming component and the pending tools, then
// renderInitialMessages). Switching re-asks the new project's trust and redraws
// the untrusted-project warning, because the initial render is what draws it.
func TestSwitchSessionResetsTheTranscriptState(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// State a previous session left behind.
	app.loadedResourcesContainer.AddChild(tui.NewText("old skill", 0, 0, nil))
	app.pendingMessages.AddChild(tui.NewText("queued while compacting", 0, 0, nil))
	app.transcript.StreamingComponent = NewAssistantMessageComponent(
		&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "half"}}},
		false, app.transcript.MarkdownTheme, "", 1, nil)
	app.transcript.pendingTools["stale"] = &ToolExecutionComponent{}
	app.queue.compactionQueuedMessages = []CompactionQueuedMessage{{Text: "queued for compaction", Mode: "steer"}}

	project := makeTrustRequiringProject(t, "alpha-theme")
	target := makePersistedSession(t, project, "from the other project")

	if _, err := app.switchSession(context.Background(), target.GetSessionFile(), ""); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}

	if len(app.loadedResourcesContainer.Children) != 0 {
		t.Errorf("loaded resources kept %d children", len(app.loadedResourcesContainer.Children))
	}
	if len(app.pendingMessages.Children) != 0 {
		t.Errorf("pending messages kept %d children", len(app.pendingMessages.Children))
	}
	if app.transcript.StreamingComponent != nil {
		t.Error("the streaming component survived the switch")
	}
	if len(app.queue.compactionQueuedMessages) != 0 {
		t.Errorf("compaction queue survived the switch: %v", app.queue.compactionQueuedMessages)
	}
	if len(app.transcript.pendingTools) != 0 {
		t.Errorf("pending tools survived the switch: %v", app.transcript.pendingTools)
	}

	// The untrusted project's warning comes back with the initial render — the
	// switch re-resolved trust for that directory (D160).
	rendered := app.chat.Render(120)
	if !strings.Contains(strings.Join(rendered, "\n"), "This project is not trusted") {
		t.Errorf("no trust warning after switching into an untrusted project:\n%s", strings.Join(rendered, "\n"))
	}
	// And the new session's messages are on screen.
	if !strings.Contains(strings.Join(rendered, "\n"), "from the other project") {
		t.Errorf("the switched-to session was not rendered:\n%s", strings.Join(rendered, "\n"))
	}
}

// A session replacement must re-render the loaded-resources area, not only
// clear it. Upstream rebindCurrentSession runs renderCurrentSessionState (which
// clears the container) and then bindCurrentSessionExtensions, whose last step
// is showLoadedResources. Without the second half, /new and a resume leave the
// Context/Skills sections blank until the next /reload.
func TestSessionReplacementReshowsLoadedResources(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.options.QuietStartup = coding.QuietStartupSetting{}
	app.Init(context.Background())

	// A skill the new session's resource loader picks up from the agent dir (the
	// same path TestReloadNowRereadsResources exercises).
	skillDir := filepath.Join(app.options.AgentDir, "skills", "tdd")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: tdd\ndescription: Test-driven development\n---\n\nWrite the test first.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The previous session's listing is on screen when the replacement starts.
	app.loadedResourcesContainer.AddChild(tui.NewText("stale resources", 0, 0, nil))

	if _, err := app.sessionNew(context.Background()); err != nil {
		t.Fatalf("sessionNew: %v", err)
	}

	rendered := coding.StripAnsi(strings.Join(app.loadedResourcesContainer.Render(80), "\n"))
	if strings.Contains(rendered, "stale resources") {
		t.Fatalf("the previous session's resources survived the reset:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Skills") || !strings.Contains(rendered, "tdd") {
		t.Fatalf("the new session's loaded resources were not re-rendered:\n%s", rendered)
	}
}
