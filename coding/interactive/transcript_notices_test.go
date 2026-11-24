package interactive

import (
	"encoding/json"
	"strconv"
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

// TestTranscriptNotices covers the cache/compaction notices.
func TestTranscriptNotices(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, false)

	// The setting gate lives in MaybeShowCacheMissNotice (upstream's
	// addCacheMissNotice is unconditional).
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})
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
	maxInt64 := int64(^uint64(0) >> 1)
	minutes := maxInt64 / 60000
	if maxInt64%60000 >= 30000 {
		minutes++
	}
	renderer.AddCacheMissNotice(coding.CacheMiss{MissedTokens: 30000, MissedCost: 0.5, IdleMs: maxInt64})
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Cache miss after "+strconv.Itoa(int(minutes))+"m idle") {
		t.Fatalf("far-future idle notice = %q", lines)
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
	// D406: a live compaction also reports how long it took, in the same form as
	// the worked-for line's duration.
	renderer.AddCompactionCostNotice(CompactionCostNotice{Kind: "compaction", Usage: ai.Usage{
		Input: 1000, Output: 500, CacheRead: 500, Cost: ai.UsageCost{Total: 0.0315},
	}, Duration: 6*time.Minute + 18*time.Second})
	lines = strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Compaction: 6m 18s · 2.0k tokens billed (~$0.03)") {
		t.Fatalf("timed compaction notice = %q", lines)
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
		for range count {
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
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})
	renderer.MaybeShowThinkingDropNotice(message)
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Anthropic dropped 2 thinking blocks") {
		t.Fatalf("notice = %q", lines)
	}

	// With a branch whose last assistant already dropped >= count: no notice.
	dir := t.TempDir()
	manager := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{
		SessionDir: dir, Persist: new(false),
	})
	manager.AppendMessage(ai.Message(&ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{diagnostic(2)}}))
	renderer2 := NewTranscriptRenderer(&tui.Container{}, nil, settings, nil, manager)
	renderer2.MaybeShowThinkingDropNotice(message)
	if len(renderer2.Chat.Children) != 0 {
		t.Fatal("notice shown when the previous count matched")
	}
}

// TestTranscriptNoticesRepaintAfterSystemThemeColorsArrive pins upstream's
// ThemedText notices (interactive-mode.ts:4210 addCacheWarmingUsage, :4226
// addCompactionCostNotice, :4268 maybeShowThinkingDropNotice, :4301
// addCacheMissNotice). The notice is built while the system theme still lacks
// the terminal's reported colors: the controller marks them pending before the
// first load, and a resumed session replays its transcript during that window.
// Upstream rebuilds the string at render, so the notice colors in once the
// terminal answers; pier baked the pending palette and stayed uncolored.
func TestTranscriptNoticesRepaintAfterSystemThemeColorsArrive(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, false)
	settings.SetShowCacheMissNotices(true)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		InitTheme("dark", false)
	})
	MarkSystemTerminalColorsPending()
	InitTheme(SystemThemeName, false)
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})

	transformations, _ := json.Marshal(map[string]any{"transformations": []map[string]any{{"type": "thinking_dropped"}}})
	renderer.AddCacheWarmingUsage(&coding.SessionEntry{
		Type: "usage", Kind: "cache_warm", Usage: &ai.Usage{Cost: ai.UsageCost{Total: 0.0012}},
	})
	renderer.AddCompactionCostNotice(CompactionCostNotice{
		Kind: "compaction", Usage: ai.Usage{Input: 140000, Cost: ai.UsageCost{Total: 0.03}},
	})
	renderer.AddCacheMissNotice(coding.CacheMiss{MissedTokens: 30000, MissedCost: 0.5})
	renderer.MaybeShowThinkingDropNotice(&ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{{
		Type: "anthropic_input_transformations", Details: transformations,
	}}})

	markers := []string{
		"Cache warmed: $0.001",
		"Compaction: 140k tokens billed (~$0.03)",
		"Cache miss: 30k tokens re-billed (~$0.50)",
		"Anthropic dropped 1 thinking block (details in session)",
	}
	render := func() map[string]string {
		lines := renderChat(t, renderer.Chat)
		found := map[string]string{}
		for _, marker := range markers {
			for _, line := range lines {
				if strings.Contains(coding.StripAnsi(line), marker) {
					found[marker] = line
				}
			}
		}
		return found
	}
	pending := render()
	for _, marker := range markers {
		line, ok := pending[marker]
		if !ok {
			t.Fatalf("notice %q not rendered", marker)
		}
		if strings.Contains(line, "\x1b[38;") {
			t.Fatalf("pending system theme already colored %q: %q", marker, line)
		}
	}

	// The terminal reports its colors: the controller reloads the theme and the
	// app invalidates the UI (interactivemode_run.go OnThemeChange).
	background := tui.RgbColor{R: 24, G: 28, B: 34}
	SetSystemTerminalColors(tui.TerminalColors{Background: &background})
	InitTheme(SystemThemeName, false)
	renderer.Chat.Invalidate()
	reported := render()
	for _, marker := range markers {
		line, ok := reported[marker]
		if !ok {
			t.Fatalf("notice %q lost its text", marker)
		}
		if !strings.Contains(line, "\x1b[38;") {
			t.Fatalf("notice %q stayed uncolored after the terminal reported colors: %q", marker, line)
		}
		if coding.StripAnsi(line) != coding.StripAnsi(pending[marker]) {
			t.Fatalf("notice %q text changed: %q -> %q", marker, pending[marker], line)
		}
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

func (s *transcriptTestSession) GetRetryAttempt() int { return s.retryAttempt }

func (s *transcriptTestSession) GetModelPriceSource() coding.ModelPriceSource {
	return nil
}

// TestTranscriptToolCalls covers the assistant tool-call rendering and the
// pending-tool matching.
func TestTranscriptToolCalls(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.Session = &transcriptTestSession{}
	renderer.SessionInfo = coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})

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
			Type: "message",
			ID:   id, Timestamp: stamp(ms),
			Message: encoded,
		}
	}
	bash := coding.CreateBashExecutionMessage("echo hi", "hi", new(0), false, false, "", false, 3000, nil)
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
	bashRow := position("! echo hi")
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
			Type: "message",
			ID:   id, Timestamp: stamp(ms),
			Message: encoded,
		}
	}
	// The command started at 1500 and its entry was appended at 2000, after a
	// message stamped in the same millisecond as the start.
	bash := coding.CreateBashExecutionMessage("echo hi", "hi", new(0), false, false, "", false, 1500, nil)
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
	bashRow := position("! echo hi")
	askRow := position("next ask")
	if bashRow < 0 || askRow < 0 {
		t.Fatalf("missing frames (bash %d, ask %d):\n%s", bashRow, askRow, strings.Join(lines, "\n"))
	}
	if bashRow > askRow {
		t.Fatalf("the idle bash frame moved past the next message (row %d, ask row %d):\n%s",
			bashRow, askRow, strings.Join(lines, "\n"))
	}
}

// The drop notice names the provider and the noun for each diagnostic kind, and
// includes the reasons the API gave: OpenAI reports withheld reasoning items as
// `reasoning`/`removed`, Anthropic withheld thinking blocks
// (interactive-mode.ts getDroppedReasoning, v1.6.0).
func TestDroppedReasoningCoversBothProviders(t *testing.T) {
	anthropic := &ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{{
		Type:    "anthropic_input_transformations",
		Details: []byte(`{"transformations":[{"type":"thinking_dropped","reason":"signature"}]}`),
	}}}
	dropped := GetDroppedReasoning(anthropic)
	if dropped == nil || dropped.Provider != "Anthropic" || dropped.Count != 1 ||
		dropped.Singular != "thinking block" || len(dropped.Reasons) != 1 {
		t.Fatalf("anthropic = %+v", dropped)
	}

	openai := &ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{{
		Type:    "openai_input_transformations",
		Details: []byte(`{"transformations":[{"type":"reasoning","reason":"prefix_mismatch"},{"type":"reasoning","reason":"org"}]}`),
	}}}
	dropped = GetDroppedReasoning(openai)
	if dropped == nil || dropped.Provider != "OpenAI" || dropped.Count != 2 ||
		dropped.Plural != "reasoning items" || len(dropped.Reasons) != 2 {
		t.Fatalf("openai = %+v", dropped)
	}

	// An unrelated diagnostic reports nothing.
	if got := GetDroppedReasoning(&ai.AssistantMessage{Diagnostics: []ai.AssistantMessageDiagnostic{{
		Type: "other", Details: []byte(`{"transformations":[{"type":"reasoning"}]}`),
	}}}); got != nil {
		t.Fatalf("unrelated diagnostic = %+v", got)
	}
}
