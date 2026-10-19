package interactive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

func newTranscriptTestRenderer(t *testing.T, showCacheMiss bool) (*TranscriptRenderer, *coding.SettingsManager) {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	settings.SetShowCacheMissNotices(showCacheMiss)
	renderer := NewTranscriptRenderer(&tui.Container{}, nil, settings, nil, nil)
	return renderer, settings
}

func renderChat(t *testing.T, chat *tui.Container) []string {
	t.Helper()
	lines := chat.Render(100)
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " ")
	}
	return lines
}

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

	renderer.AddMessageToChat(coding.CreateBashExecutionMessage("ls -la", "file.txt", intPtr(0), false, false, "", false, 1, nil), false)
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

// TestTranscriptNotices covers the cache/compaction notices.
func TestTranscriptNotices(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, false)

	// The setting gate lives in MaybeShowCacheMissNotice (upstream's
	// addCacheMissNotice is unconditional).
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: boolPtr(false)})
	renderer.MaybeShowCacheMissNotice(&ai.AssistantMessage{})
	if len(renderer.Chat.Children) != 0 {
		t.Fatal("notice rendered while disabled")
	}
	settings.SetShowCacheMissNotices(true)

	// Below both thresholds: nothing.
	renderer.AddCacheMissNotice(coding.CacheMiss{MissedTokens: 100, MissedCost: 0.001})
	if len(renderer.Chat.Children) != 0 {
		t.Fatal("insignificant notice rendered")
	}
	// Token threshold.
	renderer.AddCacheMissNotice(coding.CacheMiss{MissedTokens: 25000, MissedCost: 0.02})
	if len(renderer.Chat.Children) != 2 {
		t.Fatalf("children = %d", len(renderer.Chat.Children))
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Cache miss: 25k tokens re-billed (~$0.02)") {
		t.Fatalf("notice = %q", lines)
	}
	// Model-switch label.
	renderer.AddCacheMissNotice(coding.CacheMiss{MissedTokens: 30000, MissedCost: 0.5, ModelChanged: true})
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Cache miss after model switch") {
		t.Fatalf("model switch notice = %q", lines)
	}
	// Idle label (rounded minutes).
	renderer.AddCacheMissNotice(coding.CacheMiss{MissedTokens: 30000, MissedCost: 0.5, IdleMs: 10 * 60 * 1000})
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Cache miss after 10m idle") {
		t.Fatalf("idle notice = %q", lines)
	}

	// Compaction cost notices.
	renderer.AddCompactionCostNotice(CompactionCostNotice{Kind: "compaction", Usage: ai.Usage{
		Input: 1000, Output: 500, CacheRead: 500, CacheWrite: 0,
		Cost: ai.UsageCost{Total: 0.0315},
	}})
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Compaction: 2.0k tokens billed (~$0.03)") {
		t.Fatalf("compaction notice = %q", lines)
	}
	// Below the cost threshold: no suffix.
	renderer.AddCompactionCostNotice(CompactionCostNotice{Kind: "branch_summary", Usage: ai.Usage{
		Input: 100, Cost: ai.UsageCost{Total: 0.001},
	}})
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Branch summary: 100 tokens billed") || strings.Contains(lines, "Branch summary: 100 tokens billed (~") {
		t.Fatalf("branch notice = %q", lines)
	}
}

// TestTranscriptThinkingDropNotice covers the dropped-thinking diagnostics.
func TestTranscriptThinkingDropNotice(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, false)
	settings.SetShowCacheMissNotices(true)

	diagnostic := func(count int) ai.AssistantMessageDiagnostic {
		transformations := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			transformations = append(transformations, map[string]any{"type": "thinking_dropped"})
		}
		details, _ := json.Marshal(map[string]any{"transformations": transformations})
		return ai.AssistantMessageDiagnostic{Type: "anthropic_input_transformations", Details: details}
	}

	message := &ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{diagnostic(2)}}
	if got := CountDroppedThinkingBlocks(message); got != 2 {
		t.Fatalf("dropped = %d", got)
	}
	other := &ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{{Type: "other"}}}
	if got := CountDroppedThinkingBlocks(other); got != 0 {
		t.Fatalf("dropped other = %d", got)
	}

	// With an empty branch the previous count is zero, so the notice shows.
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: boolPtr(false)})
	renderer.MaybeShowThinkingDropNotice(message)
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Anthropic dropped 2 thinking blocks") {
		t.Fatalf("notice = %q", lines)
	}

	// With a branch whose last assistant already dropped >= count: no notice.
	dir := t.TempDir()
	manager := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{
		SessionDir: dir, Persist: boolPtr(false),
	})
	manager.AppendMessage(ai.Message(&ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{diagnostic(2)}}))
	renderer2 := NewTranscriptRenderer(&tui.Container{}, nil, settings, nil, manager)
	renderer2.MaybeShowThinkingDropNotice(message)
	if len(renderer2.Chat.Children) != 0 {
		t.Fatal("notice shown when the previous count matched")
	}
}

// TestTranscriptRenderSessionEntries covers the entry projection.
func TestTranscriptRenderSessionEntries(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, false)
	settings.SetShowCacheMissNotices(true)

	usage := ai.Usage{Input: 10, Output: 5, Cost: ai.UsageCost{Total: 0.5}}
	entries := []coding.SessionEntry{
		{Type: "custom", CustomType: "note"},
		{Type: "usage", Kind: "cache_warm", Usage: &usage},
		{Type: "compaction", Summary: "compacted", TokensBefore: 1000, Usage: &usage},
	}
	// Custom entries without a renderer are skipped.
	renderer.RenderSessionEntries(entries, false, false)
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Compaction: ") {
		t.Fatalf("missing compaction cost notice: %q", lines)
	}
	if !strings.Contains(lines, "[compaction]") {
		t.Fatalf("missing compaction summary: %q", lines)
	}
}

// transcriptTestSession implements TranscriptSession.
type transcriptTestSession struct {
	retryAttempt int
}

func (s *transcriptTestSession) GetToolRenderers(string) *ToolRenderers { return nil }
func (s *transcriptTestSession) GetRetryAttempt() int                   { return s.retryAttempt }
func (s *transcriptTestSession) GetModelPriceSource() coding.ModelPriceSource {
	return nil
}

// TestTranscriptToolCalls covers the assistant tool-call rendering and the
// pending-tool matching.
func TestTranscriptToolCalls(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.Session = &transcriptTestSession{}
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: boolPtr(false)})

	arguments, _ := json.Marshal(map[string]any{"command": "ls"})
	assistant := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.TextContent{Text: "running"}, ai.ToolCall{ID: "t1", Name: "bash", Arguments: arguments}},
		StopReason: ai.StopStop,
	}
	renderer.RenderSessionItems([]RenderSessionItem{{Message: assistant}}, false, false)
	if len(renderer.Chat.Children) != 2 { // assistant + tool component
		t.Fatalf("children = %d", len(renderer.Chat.Children))
	}
	if len(renderer.PendingTools()) != 1 {
		t.Fatalf("pending = %d", len(renderer.PendingTools()))
	}
	if _, ok := renderer.PendingTools()["t1"]; !ok {
		t.Fatal("pending tool missing")
	}

	// The matching tool result clears the pending tool.
	toolResult := &ai.ToolResultMessage{
		ToolCallID: "t1", ToolName: "bash",
		Content: ai.UserContentList{ai.TextContent{Text: "output"}},
	}
	renderer.RenderSessionItems([]RenderSessionItem{{Message: assistant}, {Message: toolResult}}, false, false)
	if len(renderer.PendingTools()) != 0 {
		t.Fatalf("pending after result = %d", len(renderer.PendingTools()))
	}

	// An aborted assistant with a tool call renders the aborted error.
	renderer.Session = &transcriptTestSession{retryAttempt: 2}
	aborted := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "t2", Name: "bash", Arguments: arguments}},
		StopReason: ai.StopAborted,
	}
	renderer.RenderSessionItems([]RenderSessionItem{{Message: aborted}}, false, false)
	if len(renderer.PendingTools()) != 0 {
		t.Fatal("aborted tool should not stay pending")
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Aborted after 2 retry attempts") {
		t.Fatalf("aborted notice = %q", lines)
	}

	// An errored assistant renders the error message.
	errorMessage := "boom"
	errored := &ai.AssistantMessage{
		Content:      ai.ContentList{ai.ToolCall{ID: "t3", Name: "bash", Arguments: arguments}},
		StopReason:   ai.StopError,
		ErrorMessage: &errorMessage,
	}
	renderer.RenderSessionItems([]RenderSessionItem{{Message: errored}}, false, false)
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "boom") {
		t.Fatalf("error notice = %q", lines)
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

func intPtr(value int) *int { return &value }

// A `!` command run during an agent turn is recorded when it completes, after
// the entries the turn produced while it ran, but the live transcript showed
// the frame at its completion point. Replaying the raw entry order drew the
// frame at the end of the turn instead (D248).
func TestReplayedBashFrameKeepsItsPositionAmongTheTurn(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)

	stamp := func(ms int64) string {
		return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	}
	messageEntry := func(id string, message ai.Message, ms int64) coding.SessionEntry {
		encoded, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		return coding.SessionEntry{
			Type:             "message",
			SessionEntryBase: coding.SessionEntryBase{ID: id, Timestamp: stamp(ms)},
			Message:          encoded,
		}
	}
	bash := coding.CreateBashExecutionMessage("echo hi", "hi", intPtr(0), false, false, "", false, 3000, nil)
	entries := []coding.SessionEntry{
		messageEntry("u1", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "start"}}, 1000),
		messageEntry("a1", &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "first pass"}}, StopReason: ai.StopStop}, 2000),
		messageEntry("a2", &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "second pass"}}, StopReason: ai.StopStop}, 4000),
		// Deferred while the turn ran, so it is appended at the turn's end
		// even though the command completed before "second pass" was written.
		messageEntry("b1", bash, 5000),
	}
	renderer.RenderSessionEntries(entries, false, false)

	lines := renderChat(t, renderer.Chat)
	position := func(text string) int {
		for index, line := range lines {
			if strings.Contains(line, text) {
				return index
			}
		}
		return -1
	}
	bashRow := position("$ echo hi")
	secondRow := position("second pass")
	if bashRow < 0 || secondRow < 0 {
		t.Fatalf("missing frames (bash %d, assistant %d):\n%s", bashRow, secondRow, strings.Join(lines, "\n"))
	}
	if bashRow > secondRow {
		t.Fatalf("the bash frame replayed at the end of the turn (row %d, assistant row %d):\n%s",
			bashRow, secondRow, strings.Join(lines, "\n"))
	}
}

// A `!` command's frame never moves later than its entry: a message appended
// after the command started but stamped in the same millisecond keeps the
// recorded order (D248).
func TestReplayedIdleBashFrameStaysInPlace(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	stamp := func(ms int64) string {
		return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	}
	messageEntry := func(id string, message ai.Message, ms int64) coding.SessionEntry {
		encoded, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		return coding.SessionEntry{
			Type:             "message",
			SessionEntryBase: coding.SessionEntryBase{ID: id, Timestamp: stamp(ms)},
			Message:          encoded,
		}
	}
	// The command started at 1500 and its entry was appended at 2000, after a
	// message stamped in the same millisecond as the start.
	bash := coding.CreateBashExecutionMessage("echo hi", "hi", intPtr(0), false, false, "", false, 1500, nil)
	entries := []coding.SessionEntry{
		messageEntry("a1", &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "first pass"}}, StopReason: ai.StopStop}, 1000),
		messageEntry("b1", bash, 2000),
		messageEntry("u2", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "next ask"}}, 1500),
	}
	renderer.RenderSessionEntries(entries, false, false)

	lines := renderChat(t, renderer.Chat)
	position := func(text string) int {
		for index, line := range lines {
			if strings.Contains(line, text) {
				return index
			}
		}
		return -1
	}
	bashRow := position("$ echo hi")
	askRow := position("next ask")
	if bashRow < 0 || askRow < 0 {
		t.Fatalf("missing frames (bash %d, ask %d):\n%s", bashRow, askRow, strings.Join(lines, "\n"))
	}
	if bashRow > askRow {
		t.Fatalf("the idle bash frame moved past the next message (row %d, ask row %d):\n%s",
			bashRow, askRow, strings.Join(lines, "\n"))
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
			Type:             "message",
			SessionEntryBase: coding.SessionEntryBase{ID: id, Timestamp: stamp(ms)},
			Message:          encoded,
		}
	}
	entries := []coding.SessionEntry{
		messageEntry("u1", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "ask"}}, 1000),
		messageEntry("b1", coding.CreateBashExecutionMessage("echo hi", "hi", intPtr(0), false, false, "", false, 2000, nil), 2000),
		messageEntry("b2", coding.CreateBashExecutionMessage("secret", "", intPtr(0), false, false, "", true, 3000, nil), 3000),
	}
	renderer.RenderSessionEntries(entries, false, true)

	for index, want := range []string{"!!secret", "!echo hi", "ask"} {
		editor.HandleInput("\x1b[A")
		if got := editor.GetText(); got != want {
			t.Fatalf("history entry %d = %q, want %q", index, got, want)
		}
	}
}
