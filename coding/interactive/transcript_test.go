package interactive

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// TestTranscriptUserMessages covers the user-message rendering paths.
func TestTranscriptUserMessages(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.MarkdownTheme = nil

	// Plain user message: no leading spacer on an empty chat.
	renderer.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}, false)
	chat := renderer.Chat
	if len(chat.Children) != 1 {
		t.Fatalf("children = %d", len(chat.Children))
	}

	// A second user message adds a spacer first.
	renderer.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "again"}}, false)
	if len(chat.Children) != 3 {
		t.Fatalf("children = %d", len(chat.Children))
	}

	// Empty content is skipped.
	before := len(chat.Children)
	renderer.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: ""}}, false)
	if len(chat.Children) != before {
		t.Fatal("empty user message rendered")
	}

	if got := renderer.GetUserMessageText(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "abc"}}); got != "abc" {
		t.Fatalf("text = %q", got)
	}
	blocks := ai.StringOrBlocks{Blocks: ai.ContentList{
		ai.TextContent{Text: "one"}, ai.TextContent{Text: "two"},
	}}
	if got := renderer.GetUserMessageText(&ai.UserMessage{Content: blocks}); got != "onetwo" {
		t.Fatalf("block text = %q", got)
	}
	if got := renderer.GetUserMessageText(&ai.AssistantMessage{}); got != "" {
		t.Fatalf("assistant text = %q", got)
	}

	// History population.
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	renderer.Editor = editor
	renderer.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "remember me"}}, true)
	editor.HandleInput("\x1b[A")
	if editor.GetText() != "remember me" {
		t.Fatalf("history navigation = %q", editor.GetText())
	}
}

// TestTranscriptSkillBlock covers the skill-block split.
func TestTranscriptSkillBlock(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	text := "<skill name=\"debug\" location=\"/skills/debug/SKILL.md\">\ninstructions\n</skill>\n\nuser question"
	renderer.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: text}}, false)
	// skill component + spacer + user message
	if len(renderer.Chat.Children) != 3 {
		t.Fatalf("children = %d", len(renderer.Chat.Children))
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "[skill]") || !strings.Contains(lines, "user question") {
		t.Fatalf("skill render = %q", lines)
	}
}

// TestTranscriptSpecialMessages covers the custom-role messages.
func TestTranscriptSpecialMessages(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)

	renderer.AddMessageToChat(coding.CreateBashExecutionMessage("ls -la", "file.txt", new(0), false, false, "", false, 1, nil), false)
	if len(renderer.Chat.Children) != 1 {
		t.Fatalf("bash children = %d", len(renderer.Chat.Children))
	}
	bashLines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(bashLines, "ls -la") {
		t.Fatalf("bash render = %q", bashLines)
	}

	renderer.AddMessageToChat(coding.CreateCompactionSummaryMessage("compacted", 12000, 2), false)
	if len(renderer.Chat.Children) != 3 { // spacer + component
		t.Fatalf("compaction children = %d", len(renderer.Chat.Children))
	}

	renderer.AddMessageToChat(coding.CreateBranchSummaryMessage("branched", "abc", 3), false)
	if len(renderer.Chat.Children) != 5 {
		t.Fatalf("branch children = %d", len(renderer.Chat.Children))
	}

	// Custom messages only render when display is true.
	before := len(renderer.Chat.Children)
	renderer.AddMessageToChat(coding.CreateCustomMessage("note", json.RawMessage(`"hidden"`), false, nil, 4), false)
	if len(renderer.Chat.Children) != before {
		t.Fatal("hidden custom message rendered")
	}
	renderer.AddMessageToChat(coding.CreateCustomMessage("note", json.RawMessage(`"shown"`), true, nil, 5), false)
	if len(renderer.Chat.Children) != before+1 {
		t.Fatalf("custom children = %d", len(renderer.Chat.Children))
	}

	// Assistant, toolResult and system messages.
	renderer.AddMessageToChat(&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop}, false)
	if len(renderer.Chat.Children) != before+2 {
		t.Fatalf("assistant children = %d", len(renderer.Chat.Children))
	}
	renderer.AddMessageToChat(&ai.ToolResultMessage{ToolCallID: "t"}, false)
	renderer.AddMessageToChat(&ai.SystemMessage{}, false)
	if len(renderer.Chat.Children) != before+2 {
		t.Fatal("toolResult/system rendered")
	}
}

// TestTranscriptStatusLines covers showStatus/showManagedToolStatus.
// TestTranscriptThemedLinesFollowTheTheme pins the stale-color class: the
// status/error/warning lines re-evaluate their theme color on the next render
// (upstream renders them through ThemedText). The system theme receiving the
// terminal's colors after startup used to leave them with the colors they were
// built with.
func TestTranscriptThemedLinesFollowTheTheme(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.ShowStatus("Session compacted 38 times")
	// The error/warning lines are built by the run wiring, which shares the
	// chat container.
	wiring := &RunWiring{Chat: renderer.Chat, Display: &DisplayOptions{OutputPad: 1}}
	wiring.ShowChatError("boom")
	wiring.ShowChatWarning("careful")
	wiring.ShowNewVersionNotification(LatestRelease{Version: "v1.1.0", URL: "https://example.com/releases/v1.1.0"}, false)

	dark := strings.Join(renderChat(t, renderer.Chat), "\n")

	InitTheme("light", false)
	renderer.Chat.Invalidate()
	light := strings.Join(renderChat(t, renderer.Chat), "\n")

	if dark == light {
		t.Fatalf("themed chat lines did not re-render after a theme change:\n%s", dark)
	}
	for _, text := range []string{"Session compacted 38 times", "Error: boom", "Warning: careful", "New version v1.1.0 is available"} {
		if !strings.Contains(light, text) {
			t.Fatalf("missing %q after the theme change: %q", text, light)
		}
	}
	// The message update path rebuilds through the same component.
	renderer.ShowStatus("Session compacted 39 times")
	renderer.Chat.Invalidate()
	updated := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(updated, "Session compacted 39 times") || strings.Contains(updated, "38 times") {
		t.Fatalf("status update did not replace the text: %q", updated)
	}
}

func TestTranscriptStatusLines(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)

	renderer.ShowStatus("first")
	if len(renderer.Chat.Children) != 2 { // spacer + text
		t.Fatalf("children = %d", len(renderer.Chat.Children))
	}
	// A consecutive status replaces the previous text.
	renderer.ShowStatus("second")
	if len(renderer.Chat.Children) != 2 {
		t.Fatalf("children after replace = %d", len(renderer.Chat.Children))
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "second") || strings.Contains(lines, "first") {
		t.Fatalf("status render = %q", lines)
	}
	// A non-status child in between appends a new status.
	renderer.Chat.AddChild(tui.NewText("interruption", 0, 0, nil))
	renderer.ShowStatus("third")
	if len(renderer.Chat.Children) != 5 {
		t.Fatalf("children after append = %d", len(renderer.Chat.Children))
	}

	managed, _ := newTranscriptTestRenderer(t, false)
	managed.ShowManagedToolStatus(ManagedToolStatus{Type: "info", Message: "installing"})
	if len(managed.Chat.Children) != 2 { // spacer + text
		t.Fatalf("managed children = %d", len(managed.Chat.Children))
	}
	managed.ShowManagedToolStatus(ManagedToolStatus{Type: "warning", Message: "slow"})
	lines = strings.Join(renderChat(t, managed.Chat), "\n")
	if !strings.Contains(lines, "Warning: slow") {
		t.Fatalf("managed render = %q", lines)
	}
}

// A footer-updating transcript rebuild also refreshes the editor border
// (upstream renderSessionItems).
func TestRenderSessionItemsUpdatesEditorBorder(t *testing.T) {
	renderer := &TranscriptRenderer{Chat: &tui.Container{}, Display: &DisplayOptions{}}
	called := 0
	renderer.UpdateEditorBorderColor = func() { called++ }
	renderer.RenderSessionItems(nil, true, false)
	if called != 1 {
		t.Fatalf("border updates = %d; want 1", called)
	}
	renderer.RenderSessionItems(nil, false, false)
	if called != 1 {
		t.Fatalf("border updates = %d; want still 1", called)
	}
}

// A continued session restores its `!` commands to the editor history, like its
// prompts: upstream only replays user messages, so the command was lost after a
// reload (D250).
func TestTranscriptRestoresBashCommandHistory(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	renderer.Editor = editor

	stamp := func(ms int64) string {
		return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	}
	messageEntry := func(id string, message ai.Message, ms int64) coding.SessionEntry {
		encoded, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		return coding.SessionEntry{
			Type: "message",
			ID:   id, Timestamp: stamp(ms),
			Message: encoded,
		}
	}
	entries := []coding.SessionEntry{
		messageEntry("u1", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "ask"}}, 1000),
		messageEntry("b1", coding.CreateBashExecutionMessage("echo hi", "hi", new(0), false, false, "", false, 2000, nil), 2000),
		messageEntry("b2", coding.CreateBashExecutionMessage("secret", "", new(0), false, false, "", true, 3000, nil), 3000),
	}
	renderer.RenderSessionEntries(entries, false, true)

	for index, want := range []string{"!!secret", "!echo hi", "ask"} {
		editor.HandleInput("\x1b[A")
		if got := editor.GetText(); got != want {
			t.Fatalf("history entry %d = %q, want %q", index, got, want)
		}
	}
}

// Reference: pi v1.4.0 (4a080252), interactive-mode.ts renderInitialMessages
// counts all persisted compactions, not just the active branch. The Go port
// need not materialize a full descriptor snapshot to compute that number.
func TestTranscriptCompactionNoticeDoesNotCopyHistory(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
	last := ""
	for range 512 {
		last = manager.AppendCustomEntry("hidden", nil)
	}
	manager.AppendCompaction("old branch", last, 100, nil, false, nil)
	if err := manager.Branch(last); err != nil {
		t.Fatal(err)
	}
	manager.AppendCompaction("current branch", last, 100, nil, false, nil)
	manager.Projection()
	renderer.SessionInfo = manager
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	renderer.RenderInitialMessages()
	runtime.ReadMemStats(&after)
	// The active window is tiny. Allow component construction, but not a
	// roughly 225 KiB full-history snapshot just to count two compactions.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64*1024 {
		t.Fatalf("replaying a compacted window allocated %d bytes, want at most 65536", allocated)
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Session compacted 2 times") {
		t.Fatalf("off-branch compaction was not counted: %s", lines)
	}
}

// The reference's custom-message component accepts string or block content and
// joins the text blocks (pi v1.5.0 f9a26365
// modes/interactive/components/custom-message.ts rebuild, which the port's
// CustomMessagePayload.Blocks already models):
//
//	text = typeof this.message.content === "string"
//		? this.message.content
//		: this.message.content.filter((c) => c.type === "text").map((c) => c.text).join("\n");
//
// A block-array message is the same message the model reads through
// convertCustomMessage's customContentToBlocks, so the transcript must show
// that text instead of an empty box.
func TestTranscriptCustomMessageRendersTextBlocks(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.MarkdownTheme = nil
	message := coding.CreateCustomMessage("notice",
		[]byte(`[{"type":"text","text":"first block"},{"type":"text","text":"second block"}]`), true, nil, 1000)
	renderer.AddMessageToChat(message, false)
	rendered := strings.Join(renderChat(t, renderer.Chat), "\n")
	for _, want := range []string{"first block", "second block"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("transcript lost %q; rendered:\n%s", want, rendered)
		}
	}
}

// The custom message box writes the label, then the body. The body goes through
// tui.SanitizeUntrustedText (D342: model- and extension-generated session text
// renders as data), but the label was interpolated raw:
//
//	label := theme.Fg("customMessageLabel", "\x1b[1m["+c.message.CustomType+"]\x1b[22m")
//
// customType comes from an extension's sendMessage call and is persisted in the
// session file, so a value carrying an escape sequence executed a terminal
// control function (an OSC 0 title set, a mode change) in the middle of the
// frame, and the reference does the same. Treat the label as data like the body.
func TestTranscriptCustomMessageLabelIsSanitized(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.MarkdownTheme = nil
	message := coding.CreateCustomMessage("notice\x1b]0;pwned\x07", []byte(`"body"`), true, nil, 1000)
	renderer.AddMessageToChat(message, false)
	rendered := strings.Join(renderChat(t, renderer.Chat), "\n")

	if strings.Contains(rendered, "\x1b]0;pwned") {
		t.Fatalf("the customType control sequence reached the terminal: %q", rendered)
	}
	// The label itself is still visible, just without the sequence.
	if !strings.Contains(rendered, "[notice") {
		t.Fatalf("the label disappeared; rendered:\n%s", rendered)
	}
}

// A goal lifecycle entry renders its card both live (a freshly appended entry,
// whose Raw line is not populated) and when replayed from disk. The renderer
// receives the entry's payload, matching the extension entry-renderer contract.
func TestTranscriptRendersGoalEntryCards(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"operation": "create",
		"goal": map[string]any{
			"version": 1, "id": "g1", "revision": 1, "objective": "ship the widget",
			"phase": "active", "createdAt": 1, "updatedAt": 1,
		},
		"timestamp": 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name  string
		entry *coding.SessionEntry
	}{
		{
			name: "live",
			entry: &coding.SessionEntry{
				Type: "custom", ID: "e1", CustomType: "pi-goal", Data: payload, Timestamp: "1970-01-01T00:00:00.001Z",
			},
		},
		{
			name:  "replayed",
			entry: loadedGoalEntry(t, payload),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			renderer, _ := newTranscriptTestRenderer(t, false)
			renderer.EntryRenderer = GoalEntryRenderer
			renderer.AddCustomEntryToChat(testCase.entry)
			rendered := strings.Join(renderChat(t, renderer.Chat), "\n")
			if !strings.Contains(rendered, "Goal created") || !strings.Contains(rendered, "ship the widget") {
				t.Fatalf("goal entry card missing: %q", rendered)
			}
		})
	}
}

// loadedGoalEntry parses a persisted custom entry the way the session loader
// does: the payload lands in Data and the whole line in Raw.
func loadedGoalEntry(t *testing.T, payload json.RawMessage) *coding.SessionEntry {
	t.Helper()
	line := `{"type":"custom","customType":"pi-goal","data":` + string(payload) +
		`,"id":"e1","timestamp":"1970-01-01T00:00:00.001Z"}`
	fileEntry, err := coding.UnmarshalFileEntry(line)
	if err != nil {
		t.Fatal(err)
	}
	return fileEntry.Entry
}

// D538: a background call's completion notice renders as a [background] block,
// collapsed to its title, not as a message the user typed, and it is not added
// to the editor's history, where Up would recall it as the user's own input.
func TestTranscriptBackgroundNotice(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	renderer.Editor = editor
	notice := agent.CompletionNotice(agent.ToolCallHandle{ID: "bg-77", ToolName: "bash", OutputPath: "/tmp/out.log"},
		agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "BUILD OK"}}})
	renderer.AddMessageToChat(notice, true)
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "[background]") || !strings.Contains(lines, "bg-77 (bash) finished") {
		t.Fatalf("notice render = %q", lines)
	}
	if strings.Contains(lines, "BUILD OK") {
		t.Fatalf("a collapsed notice shows its body: %q", lines)
	}
	editor.HandleInput("\x1b[A")
	if got := editor.GetText(); got != "" {
		t.Fatalf("Up recalled the notice as typed input: %q", got)
	}
}

// D537: a self-paced /loop run carries a note for the model ("This is one run
// of a self-paced /loop…"). The transcript shows the user's prompt without it,
// so every run does not repeat two lines of instructions the user never typed.
func TestTranscriptHidesTheSelfPacedLoopNote(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	renderer.Editor = editor
	renderer.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: coding.LoopSelfPacedPrompt("check the deploy")}}, true)
	// A reloaded session's history recalls the prompt, not the note.
	editor.HandleInput("\x1b[A")
	if got := editor.GetText(); got != "check the deploy" {
		t.Fatalf("Up recalled %q", got)
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "check the deploy") {
		t.Fatalf("the prompt is missing: %q", lines)
	}
	if strings.Contains(lines, "schedule_wakeup") {
		t.Fatalf("the transcript shows the model's note: %q", lines)
	}
}
