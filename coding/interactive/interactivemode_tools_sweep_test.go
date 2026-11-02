package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// ctrl+o used to rebuild every expandable chat child before the next paint, so
// a long session froze for the whole transcript's render time (M65). The sweep
// now applies to the trailing window and catches up through MaterializeToolChunk
// on loop beats, like the ctrl+t thinking sweep.
type sweepProbeComponent struct {
	expanded bool
	calls    int
}

func (c *sweepProbeComponent) Render(int) []string { return nil }
func (c *sweepProbeComponent) Invalidate()         {}
func (c *sweepProbeComponent) SetExpanded(expanded bool) {
	c.expanded = expanded
	c.calls++
}

func newSweepProbeController(count int) (*QueueController, []*sweepProbeComponent) {
	controller := &QueueController{Chat: &tui.Container{}}
	components := make([]*sweepProbeComponent, count)
	for index := range components {
		components[index] = &sweepProbeComponent{}
		controller.Chat.AddChild(components[index])
	}
	return controller, components
}

func TestSetToolsExpandedRebuildsOnlyTheVisibleWindow(t *testing.T) {
	controller, components := newSweepProbeController(100)
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)

	synchronous := 0
	for _, component := range components {
		if component.calls > 0 {
			synchronous++
		}
	}
	if synchronous == 0 {
		t.Fatal("the visible tail must expand immediately")
	}
	if synchronous > toolSweepWindowComponents {
		t.Fatalf("rebuilt %d cards synchronously, want at most %d", synchronous, toolSweepWindowComponents)
	}
	// The window is the tail: the newest cards are the ones on screen.
	for index := len(components) - synchronous; index < len(components); index++ {
		if !components[index].expanded {
			t.Fatalf("tail card %d not expanded", index)
		}
	}
	if !controller.HasToolChunk() {
		t.Fatal("the rest of the sweep must be deferred")
	}
}

func TestMaterializeToolChunkExpandsEveryDeferredCard(t *testing.T) {
	controller, components := newSweepProbeController(100)
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)

	beats := 0
	for controller.MaterializeToolChunk() {
		beats++
		if beats > 1000 {
			t.Fatal("tool sweep did not finish")
		}
	}
	if controller.HasToolChunk() {
		t.Fatal("pending tool work after draining")
	}
	for index, component := range components {
		if !component.expanded {
			t.Fatalf("card %d never expanded", index)
		}
	}
	if !expanded {
		t.Fatal("expansion state not recorded")
	}
}

func TestMaterializeToolChunkIsBounded(t *testing.T) {
	controller, components := newSweepProbeController(64)
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	before := make([]int, len(components))
	for index, component := range components {
		before[index] = component.calls
	}
	controller.MaterializeToolChunk()
	rebuilt := 0
	for index, component := range components {
		if component.calls > before[index] {
			rebuilt++
		}
	}
	if rebuilt > toolSweepChunkComponents {
		t.Fatalf("one beat rebuilt %d cards, want at most %d", rebuilt, toolSweepChunkComponents)
	}
}

// A second ctrl+o before the sweep drains applies the new state from the top;
// stale deferred work must not fight it.
func TestSetToolsExpandedRestartsPendingSweep(t *testing.T) {
	controller, components := newSweepProbeController(40)
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	controller.SetToolsExpanded(false, &expanded, nil, nil)
	for controller.MaterializeToolChunk() {
	}
	for index, component := range components {
		if component.expanded {
			t.Fatalf("card %d stayed expanded after collapsing", index)
		}
	}
	if expanded {
		t.Fatal("state not collapsed")
	}
}

// The status line still reports the new state on the same keypress.
func TestSetToolsExpandedReportsStatusImmediately(t *testing.T) {
	controller, _ := newSweepProbeController(40)
	var statuses []string
	controller.ShowStatus = func(message string) { statuses = append(statuses, message) }
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	if len(statuses) != 1 || statuses[0] != "Tool output: expanded" {
		t.Fatalf("statuses = %v", statuses)
	}
	if !controller.HasToolChunk() {
		t.Fatal("expected deferred work")
	}
}

func TestSetToolsExpandedNoOpKeepsDeferredWork(t *testing.T) {
	controller, components := newSweepProbeController(40)
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	callsBefore := 0
	for _, component := range components {
		callsBefore += component.calls
	}
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	callsAfter := 0
	for _, component := range components {
		callsAfter += component.calls
	}
	if callsAfter != callsBefore {
		t.Fatalf("no-op toggle rebuilt cards: %d -> %d", callsBefore, callsAfter)
	}
	if !controller.HasToolChunk() {
		t.Fatal("no-op toggle dropped the pending sweep")
	}
}

// Real tool cards reach the same final state after draining: the collapsed
// preview hint disappears once expanded.
func TestToolCardExpansionReachesDeferredCards(t *testing.T) {
	newRendererTestTheme(t)
	controller := &QueueController{Chat: &tui.Container{}}
	definition := bashRenderers
	output := strings.Repeat("output line\n", 40)
	var cards []*ToolExecutionComponent
	for index := range 40 {
		card := NewToolExecutionComponent("bash", "call-"+itoa(index),
			map[string]any{"command": "make build"}, ToolExecutionOptions{}, &definition, nil, "/tmp")
		card.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: output}}}, false)
		cards = append(cards, card)
		controller.Chat.AddChild(card)
	}
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	for controller.MaterializeToolChunk() {
	}
	for index, card := range cards {
		if rendered := strings.Join(card.Render(120), "\n"); strings.Contains(rendered, "to expand") {
			t.Fatalf("card %d still shows the collapsed hint", index)
		}
	}
}

// A session switch, branch navigation or compaction rebuild clears the chat
// while a sweep may still be draining. The deferred cards belong to the
// transcript that is being replaced, so the loop must stop rebuilding them
// instead of touching orphans and repainting the new transcript once per beat
// (M77).
func TestChatResetStopsBothSweeps(t *testing.T) {
	initSweepTheme(t)
	controller, components := newSweepProbeController(100)
	expanded := false
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	for range 20 {
		controller.Chat.AddChild(NewAssistantMessageComponent(nil, false, nil, "", 0, nil))
	}
	controller.UpdateThinkingBlockVisibility(true)
	if !controller.HasToolChunk() || !controller.HasThinkingChunk() {
		t.Fatal("both sweeps must be deferred for the reproduction")
	}
	calls := make([]int, len(components))
	for index, component := range components {
		calls[index] = component.calls
	}

	controller.Chat.Clear()
	controller.ResetSweeps()

	if controller.HasToolChunk() || controller.HasThinkingChunk() {
		t.Fatal("a cleared chat must not leave a pending sweep")
	}
	if controller.MaterializeToolChunk() || controller.MaterializeThinkingChunk() {
		t.Fatal("the drained sweeps must report no remaining work")
	}
	for index, component := range components {
		if component.calls != calls[index] {
			t.Fatalf("card %d was rebuilt after the chat was cleared", index)
		}
	}
}

// initSweepTheme gives components that theme their text a palette.
func initSweepTheme(t *testing.T) {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)
}

func TestWiringChatResetDropsPendingSweeps(t *testing.T) {
	initSweepTheme(t)
	for name, reset := range map[string]func(*StartupWiring){
		"clear":  func(w *StartupWiring) { w.ClearChatAndRenderInitialMessages() },
		"render": func(w *StartupWiring) { w.RenderCurrentSessionState() },
	} {
		t.Run(name, func(t *testing.T) {
			controller, _ := newSweepProbeController(100)
			expanded := false
			controller.SetToolsExpanded(true, &expanded, nil, nil)
			controller.UpdateThinkingBlockVisibility(true)
			reset(&StartupWiring{Chat: controller.Chat, ResetSweeps: controller.ResetSweeps})
			if controller.HasToolChunk() || controller.HasThinkingChunk() {
				t.Fatal("the chat reset must drop the pending sweeps")
			}
		})
	}
}

// Rapid ctrl+o toggles while a sweep drains must converge on the last requested
// state: each restart re-defers the remaining cards with the new target, and a
// finished drain rebuilds every card exactly once for that target.
func TestToolSweepConvergesAcrossRapidToggles(t *testing.T) {
	controller, components := newSweepProbeController(100)
	expanded := false
	drain := func() {
		for controller.MaterializeToolChunk() {
		}
	}
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	controller.MaterializeToolChunk()
	controller.SetToolsExpanded(false, &expanded, nil, nil)
	drain()
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	drain()
	for index, component := range components {
		if !component.expanded {
			t.Fatalf("card %d not expanded after true-false-true", index)
		}
	}

	controller.SetToolsExpanded(false, &expanded, nil, nil)
	controller.MaterializeToolChunk()
	controller.SetToolsExpanded(true, &expanded, nil, nil)
	controller.MaterializeToolChunk()
	controller.SetToolsExpanded(false, &expanded, nil, nil)
	drain()
	for index, component := range components {
		if component.expanded {
			t.Fatalf("card %d still expanded after a toggle ending collapsed", index)
		}
	}
}
