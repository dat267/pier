package interactive

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// sessionEntryToContextMessages returns entry.message itself. Transcript replay
// and model context therefore share the persisted object, including the
// identity used to match replayed assistant messages to cache-miss notices.
func TestTranscriptReplaySharesPersistedMessages(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: boolPtr(false)})
	id := manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "persisted reply"}},
		API:     ai.APIOpenAIResponses, Provider: "test", Model: "test", StopReason: ai.StopStop,
	})
	// Session creation has already resolved the provider context before replay.
	manager.Projection()
	renderer.SessionInfo = manager
	renderer.RenderInitialMessages()
	for _, child := range renderer.Chat.Children {
		if component, ok := child.(*AssistantMessageComponent); ok {
			if got := manager.MessageEntryID(component.lastMessage); got != id {
				t.Fatalf("replayed message lost persisted identity: entry ID %q, want %q", got, id)
			}
			return
		}
	}
	t.Fatal("replayed assistant component missing")
}

// Once session creation has decoded a large message, transcript construction
// should allocate its components, not decode/copy the payload a second time.
func TestTranscriptReplayDoesNotDecodeCachedLargePayload(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: boolPtr(false)})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("x", 256<<10)}, Timestamp: 1})
	manager.Projection()
	renderer.SessionInfo = manager
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	renderer.RenderInitialMessages()
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64*1024 {
		t.Fatalf("replaying a cached 256 KiB message allocated %d bytes, want at most 65536", allocated)
	}
	if len(renderer.Chat.Children) != 1 {
		t.Fatalf("user message was not replayed: %d children", len(renderer.Chat.Children))
	}
}

// The pin's renderSessionEntries uses raw retained entries, not the edited LLM
// projection. Replay must keep original text and editor history even when the
// model sees replacement content or a message is omitted from its context.
func TestTranscriptReplayKeepsOriginalsAfterContextEdits(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: boolPtr(false)})
	user := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original question"}, Timestamp: 1})
	assistant := manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "original reply"}},
		API:     ai.APIOpenAIResponses, Provider: "test", Model: "test", StopReason: ai.StopStop,
	})
	manager.AppendContextEdit(user, json.RawMessage(`{"content":"edited question"}`))
	manager.AppendContextEdit(assistant, json.RawMessage(`null`))
	before, err := ai.MarshalJSON(manager.Projection().Messages)
	if err != nil {
		t.Fatal(err)
	}
	renderer.SessionInfo = manager
	renderer.Editor = NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	renderer.RenderInitialMessages()
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "original question") || !strings.Contains(lines, "original reply") || strings.Contains(lines, "edited question") {
		t.Fatalf("replay used edited LLM content instead of original messages: %s", lines)
	}
	renderer.Editor.HandleInput("\x1b[A")
	if got := renderer.Editor.GetText(); got != "original question" {
		t.Fatalf("editor history = %q, want original question", got)
	}
	after, err := ai.MarshalJSON(manager.Projection().Messages)
	if err != nil || string(after) != string(before) {
		t.Fatalf("replay mutated model context: %s (%v), want %s", after, err, before)
	}
}

// Cache misses are keyed by persisted assistant identity in the reference's
// renderSessionItems. Re-decoding the replay used to lose that lookup.
func TestTranscriptReplayRetainsCacheMissNoticeIdentity(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, true)
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: boolPtr(false)})
	for index, usage := range []ai.Usage{
		{Input: 1000, CacheRead: 50000},
		{Input: 51000, Cost: ai.UsageCost{Input: 0.5, Total: 0.5}},
	} {
		manager.AppendMessage(&ai.AssistantMessage{
			Content: ai.ContentList{ai.TextContent{Text: "reply"}},
			API:     ai.APIOpenAIResponses, Provider: "test", Model: "test",
			StopReason: ai.StopStop, Timestamp: int64(index + 1), Usage: usage,
		})
	}
	manager.Projection()
	renderer.SessionInfo, renderer.Session = manager, &transcriptTestSession{}
	renderer.RenderInitialMessages()
	if lines := strings.Join(renderChat(t, renderer.Chat), "\n"); !strings.Contains(lines, "tokens re-billed") {
		t.Fatalf("replayed cache-miss notice lost its message identity: %s", lines)
	}
}
