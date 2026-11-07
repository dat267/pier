package interactive

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
	"github.com/dat267/pier/tui"
)

// Lazy transcript materialization: on a large session load only the last
// window of components attaches immediately (the visible bottom), and the
// rest is attached in chunks from the UI loop, so the first paint is instant
// instead of a multi-hundred-millisecond full-transcript render.

func TestLazyTranscriptDefersLargeSessions(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(false)})
	const count = 800
	for i := range count {
		manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("lazy-msg-%04d", i)}})
	}

	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
	transcript.RenderInitialMessages()

	if len(chat.Children) >= count {
		t.Fatalf("large session attached eagerly: %d children", len(chat.Children))
	}

	for transcript.MaterializeDeferred(80) {
	}
	if len(transcript.deferredComponents) != 0 {
		t.Fatalf("deferred queue not drained: %d", len(transcript.deferredComponents))
	}

	// Everything attached, in order.
	lines := strings.Join(renderChat(t, chat), "\n")
	first := strings.Index(lines, "lazy-msg-0000")
	last := strings.Index(lines, "lazy-msg-0799")
	if first == -1 || last == -1 || first > last {
		t.Fatalf("order broken: first=%d last=%d", first, last)
	}
	for _, probe := range []string{"lazy-msg-0100", "lazy-msg-0400", "lazy-msg-0700"} {
		if !strings.Contains(lines, probe) {
			t.Fatalf("missing %s after materialization", probe)
		}
	}
}

func TestLazyTranscriptSmallSessionsAttachEagerly(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(false)})
	for i := range 30 {
		manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("small-msg-%02d", i)}})
	}

	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
	transcript.RenderInitialMessages()

	if transcript.deferredComponents != nil {
		t.Fatalf("small session deferred: %d components", len(transcript.deferredComponents))
	}
	lines := strings.Join(renderChat(t, chat), "\n")
	if !strings.Contains(lines, "small-msg-00") || !strings.Contains(lines, "small-msg-29") {
		t.Fatal("small session messages missing")
	}
	if transcript.MaterializeDeferred(80) {
		t.Fatal("materialize reported work for an eager session")
	}
}

// TestLazyTranscriptLiveAppendsStayOrdered pins the interleaving rule: live
// messages appended while the deferred queue drains must stay after the
// deferred (older) entries once materialization completes.
func TestLazyTranscriptLiveAppendsStayOrdered(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(false)})
	for i := range 800 {
		manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("lazy-msg-%04d", i)}})
	}

	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
	transcript.RenderInitialMessages()

	// A live user message arrives before materialization finishes.
	transcript.AddMessageToChat(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "live-msg"}}, false)

	for transcript.MaterializeDeferred(80) {
	}
	lines := strings.Join(renderChat(t, chat), "\n")
	live := strings.Index(lines, "live-msg")
	early := strings.Index(lines, "lazy-msg-0000")
	late := strings.Index(lines, "lazy-msg-0799")
	if live == -1 || early == -1 || late == -1 || !(early < late && late < live) {
		t.Fatalf("order broken: early=%d late=%d live=%d", early, late, live)
	}
}

// TestLazyTranscriptPrerendersOffLoop drives materialization through the
// pre-render queue: the worker warms each chunk, the loop attaches only
// warmed chunks, and the transcript still ends up complete and in order.
func TestLazyTranscriptPrerendersOffLoop(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(false)})
	const count = 800
	for i := range count {
		manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("lazy-msg-%04d", i)}})
	}

	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
	queue := offloop.New()
	defer queue.Stop()
	transcript.PrerenderQueue = queue
	transcript.RenderInitialMessages()

	if len(chat.Children) >= count {
		t.Fatalf("large session attached eagerly: %d children", len(chat.Children))
	}

	deadline := time.Now().Add(10 * time.Second)
	for transcript.MaterializeDeferred(80) {
		if time.Now().After(deadline) {
			t.Fatalf("materialization did not finish: %d deferred left", len(transcript.deferredComponents))
		}
	}
	if len(transcript.deferredComponents) != 0 {
		t.Fatalf("deferred queue not drained: %d", len(transcript.deferredComponents))
	}
	if transcript.pre.busy || transcript.pre.ready {
		t.Fatalf("prerender state not settled: busy=%v ready=%v", transcript.pre.busy, transcript.pre.ready)
	}

	lines := strings.Join(renderChat(t, chat), "\n")
	first := strings.Index(lines, "lazy-msg-0000")
	last := strings.Index(lines, "lazy-msg-0799")
	if first == -1 || last == -1 || first > last {
		t.Fatalf("order broken: first=%d last=%d", first, last)
	}
}

// TestLazyTranscriptPrerenderRewarmsAfterWidthChange pins that a chunk warmed
// for one width is discarded rather than attached when the terminal width has
// moved (the render cache is keyed by width, so attaching it would just make
// the loop re-render the same components).
func TestLazyTranscriptPrerenderRewarmsAfterWidthChange(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(false)})
	for i := range 800 {
		manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("lazy-msg-%04d", i)}})
	}

	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
	queue := offloop.New()
	defer queue.Stop()
	transcript.PrerenderQueue = queue
	transcript.RenderInitialMessages()

	// Kick a warm at one width, then drive the rest at another.
	transcript.MaterializeDeferred(80)
	deadline := time.Now().Add(10 * time.Second)
	for transcript.MaterializeDeferred(100) {
		if time.Now().After(deadline) {
			t.Fatal("materialization did not finish after a width change")
		}
	}
	if len(transcript.deferredComponents) != 0 {
		t.Fatalf("deferred queue not drained: %d", len(transcript.deferredComponents))
	}
	lines := strings.Join(renderChat(t, chat), "\n")
	if !strings.Contains(lines, "lazy-msg-0000") || !strings.Contains(lines, "lazy-msg-0799") {
		t.Fatal("transcript incomplete after a width change")
	}
}

// A theme switch must reach the components still waiting in the deferred
// queue. Reproduction: with `theme: system`, an old session loads while the
// terminal-color reply is pending, so every component is built against the
// grayscale theme; the reply's full invalidate reached only the attached
// window, and an early compaction box kept a white `[compaction]` label (its
// text is colored at construction) over the correct, render-time background.
// Upstream has no deferred queue: pi v1.5.0 (f9a26365)
// modes/interactive/interactive-mode.ts renders every chat child, so its
// theme invalidate reaches the compaction component
// (components/compaction-summary-message.ts `invalidate` → `updateDisplay`).
func TestLazyTranscriptThemeChangeReachesDeferredComponents(t *testing.T) {
	for _, prerender := range []bool{false, true} {
		t.Run(fmt.Sprintf("prerender=%v", prerender), func(t *testing.T) {
			SetCustomThemesDir(t.TempDir())
			SetRegisteredThemes(nil)
			SetTrueColorSupport(true)
			SetStyleColorsEnabled(true)
			InitTheme("dark", false)
			t.Cleanup(func() { InitTheme("dark", false) })

			manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
			first := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept"}})
			manager.AppendCompaction("summary", first, 378775, nil, false, nil)
			for i := range 800 {
				manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("lazy-msg-%04d", i)}})
			}

			chat := &tui.Container{}
			transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
			if prerender {
				queue := offloop.New()
				defer queue.Stop()
				transcript.PrerenderQueue = queue
			}
			transcript.RenderInitialMessages()
			// Start a warm under the old theme, so the prerender path also has
			// an in-flight or ready chunk to discard.
			transcript.MaterializeDeferred(100)

			darkLabel := ActiveTheme().Fg("customMessageLabel", "x")
			InitTheme("light", false)
			lightLabel := ActiveTheme().Fg("customMessageLabel", "x")
			if darkLabel == lightLabel {
				t.Fatal("the label color must differ between the themes")
			}
			chat.Invalidate() // what tui.Renderer.Invalidate does on a theme change

			deadline := time.Now().Add(10 * time.Second)
			for transcript.MaterializeDeferred(100) {
				if time.Now().After(deadline) {
					t.Fatalf("materialization did not finish: %d deferred left", len(transcript.deferredComponents))
				}
			}
			var compaction *CompactionSummaryMessageComponent
			for _, child := range chat.Children {
				if c, ok := child.(*CompactionSummaryMessageComponent); ok {
					compaction = c
				}
			}
			if compaction == nil {
				t.Fatal("no compaction component in the transcript")
			}
			rendered := strings.Join(compaction.Render(100), "\n")
			labelColor := strings.TrimSuffix(lightLabel, "x\x1b[39m")
			if !strings.Contains(rendered, labelColor+"\x1b[1m[compaction]") {
				t.Fatalf("compaction label kept the old theme:\n%q\nwant prefix %q", rendered, labelColor)
			}
		})
	}
}

// ctrl+o and ctrl+t sweep the attached chat children (D343); components still
// in the deferred queue must come out in the state the sweep chose, not the one
// they were built with. Upstream has no deferred queue: pi v1.5.0 (f9a26365)
// modes/interactive/interactive-mode.ts setToolsExpanded and the thinking
// toggle update every chat child.
func TestLazyTranscriptSweepsReachDeferredComponents(t *testing.T) {
	for _, prerender := range []bool{false, true} {
		t.Run(fmt.Sprintf("prerender=%v", prerender), func(t *testing.T) {
			initSweepTheme(t)
			manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
			first := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept"}})
			manager.AppendCompaction("summary body", first, 1234, nil, false, nil)
			for i := range 500 {
				manager.AppendMessage(&ai.AssistantMessage{StopReason: ai.StopStop, Content: ai.ContentList{
					ai.ThinkingContent{Thinking: fmt.Sprintf("secret-thought-%d", i)}, ai.TextContent{Text: "answer"}}})
			}
			chat := &tui.Container{}
			transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
			if prerender {
				queue := offloop.New()
				defer queue.Stop()
				transcript.PrerenderQueue = queue
			}
			transcript.RenderInitialMessages()
			if len(transcript.deferredComponents) == 0 {
				t.Fatal("the session must load lazily for the reproduction")
			}
			// A warm in flight or ready under the old display state.
			transcript.MaterializeDeferred(100)

			controller := &QueueController{Chat: chat}
			transcript.Display.ToolOutputExpanded = true // what ctrl+o's setExpanded records
			transcript.Display.HideThinkingBlock = true  // and ctrl+t's
			expanded := false
			controller.SetToolsExpanded(true, &expanded, nil, nil)
			controller.UpdateThinkingBlockVisibility(true)
			for controller.MaterializeToolChunk() || controller.MaterializeThinkingChunk() {
			}
			deadline := time.Now().Add(10 * time.Second)
			for transcript.MaterializeDeferred(100) {
				if time.Now().After(deadline) {
					t.Fatalf("materialization did not finish: %d deferred left", len(transcript.deferredComponents))
				}
			}
			out := strings.Join(chat.Render(100), "\n")
			if strings.Contains(out, "to expand") {
				t.Error("the deferred compaction box stayed collapsed after ctrl+o")
			}
			if n := strings.Count(out, "secret-thought-"); n != 0 {
				t.Errorf("thinking still shown in %d messages after ctrl+t", n)
			}
		})
	}
}
