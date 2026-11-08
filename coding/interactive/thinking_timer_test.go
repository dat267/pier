package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

func pinThinkingClock(t *testing.T, at *time.Time) {
	t.Helper()
	saved := thinkingClock
	thinkingClock = func() time.Time { return *at }
	t.Cleanup(func() { thinkingClock = saved })
}

func renderAssistant(c *AssistantMessageComponent) string {
	return tui.StripTerminalSequences(strings.Join(c.Render(80), "\n"))
}

// D403: pier addition (pi v1.5.0 assistant-message.ts shows only the hidden
// thinking label). A collapsed thinking run that streams shows how long it
// has been thinking, ticking each second, then how long it thought once a
// later block or the end of the message arrives.
func TestCollapsedThinkingShowsHowLongItThought(t *testing.T) {
	_, _, _, _ = newEventTestDispatcher(t) // theme setup
	now := time.Unix(1000, 0)
	pinThinkingClock(t, &now)

	message := &ai.AssistantMessage{Content: ai.ContentList{ai.ThinkingContent{Thinking: "hmm"}}}
	component := NewAssistantMessageComponent(nil, true, nil, "", 1, nil)
	component.UpdateContent(message, true)
	if got := renderAssistant(component); !strings.Contains(got, "Thinking... (0s)") {
		t.Fatalf("streaming start = %q", got)
	}

	now = now.Add(12300 * time.Millisecond)
	need, next := component.AnimationFrame(now)
	if !need || next != 700*time.Millisecond {
		t.Fatalf("AnimationFrame = %v, %v; want the next whole second", need, next)
	}
	component.AnimationTick()
	if got := renderAssistant(component); !strings.Contains(got, "Thinking... (12s)") {
		t.Fatalf("after the tick = %q", got)
	}

	// Text after the run ends it.
	message = &ai.AssistantMessage{Content: ai.ContentList{ai.ThinkingContent{Thinking: "hmm"}, ai.TextContent{Text: "Answer"}}}
	component.UpdateContent(message, true)
	now = now.Add(5 * time.Second)
	component.UpdateContent(message, false)
	if got := renderAssistant(component); !strings.Contains(got, "Thought for 12.3s") || !strings.Contains(got, "Answer") {
		t.Fatalf("finished = %q", got)
	}
	if need, _ := component.AnimationFrame(now); need {
		t.Fatal("a finished run still animates")
	}

	// Expanding shows the thinking itself; collapsing again keeps the time.
	component.SetHideThinkingBlock(false)
	if got := renderAssistant(component); strings.Contains(got, "Thought for") || !strings.Contains(got, "hmm") {
		t.Fatalf("expanded = %q", got)
	}
	component.SetHideThinkingBlock(true)
	if got := renderAssistant(component); !strings.Contains(got, "Thought for 12.3s") {
		t.Fatalf("collapsed again = %q", got)
	}

	// A run first seen already followed by text has an unknown duration, even
	// when the message then streams: plain label.
	late := NewAssistantMessageComponent(nil, true, nil, "", 1, nil)
	late.UpdateContent(message, true)
	late.UpdateContent(message, false)
	if got := renderAssistant(late); strings.Contains(got, "Thought for") || strings.Contains(got, "(") {
		t.Fatalf("late-seen run = %q", got)
	}

	// A message loaded from the session never streamed here: plain label.
	loaded := NewAssistantMessageComponent(message, true, nil, "", 1, nil)
	if got := renderAssistant(loaded); !strings.Contains(got, "Thinking...") || strings.Contains(got, "Thought for") || strings.Contains(got, "(") {
		t.Fatalf("loaded = %q", got)
	}
}

// An aborted message ends its run when streaming stops, and a custom label
// keeps its text with the time after it. Two runs time separately.
func TestCollapsedThinkingTimesEachRun(t *testing.T) {
	_, _, _, _ = newEventTestDispatcher(t)
	now := time.Unix(1000, 0)
	pinThinkingClock(t, &now)
	component := NewAssistantMessageComponent(nil, true, nil, "Pondering", 1, nil)
	first := ai.ThinkingContent{Thinking: "a"}
	component.UpdateContent(&ai.AssistantMessage{Content: ai.ContentList{first}}, true)
	now = now.Add(2 * time.Second)
	component.UpdateContent(&ai.AssistantMessage{Content: ai.ContentList{first, ai.TextContent{Text: "x"}, ai.ThinkingContent{Thinking: "b"}}}, true)
	now = now.Add(65 * time.Second)
	component.UpdateContent(&ai.AssistantMessage{Content: ai.ContentList{first, ai.TextContent{Text: "x"}, ai.ThinkingContent{Thinking: "b"}}, StopReason: ai.StopAborted}, false)
	got := renderAssistant(component)
	if !strings.Contains(got, "Pondering (2.0s)") || !strings.Contains(got, "Pondering (1m 5s)") {
		t.Fatalf("runs = %q", got)
	}
}

// The renderer's animator walk finds a streaming collapsed run inside the
// transcript and its tick changes the rendered frame.
func TestCollapsedThinkingTicksThroughTheRenderer(t *testing.T) {
	_, _, _, _ = newEventTestDispatcher(t)
	now := time.Unix(1000, 0)
	pinThinkingClock(t, &now)
	component := NewAssistantMessageComponent(nil, true, nil, "", 1, nil)
	component.UpdateContent(&ai.AssistantMessage{Content: ai.ContentList{ai.ThinkingContent{Thinking: "hmm"}}}, true)
	chat := &tui.Container{}
	chat.AddChild(component)
	screen := tui.NewMainScreen(&fakeRendererTerminal{width: 80, height: 24}, false, "")
	screen.DisableAutoRender()
	screen.AddChild(chat)
	before := tui.StripTerminalSequences(strings.Join(chat.Render(80), "\n"))
	now = now.Add(3 * time.Second)
	if need, _ := screen.NextAnimation(); !need {
		t.Fatal("the transcript did not ask for a thinking frame")
	}
	after := tui.StripTerminalSequences(strings.Join(chat.Render(80), "\n"))
	if before == after || !strings.Contains(after, "Thinking... (3s)") {
		t.Fatalf("frame before %q, after %q", before, after)
	}
}
