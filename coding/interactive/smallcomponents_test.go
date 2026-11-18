package interactive

import (
	"context"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"strings"
	"testing"
	"time"
)

func TestDockSpacerAtInit(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	if len(app.widgetAbove.Children) != 1 {
		t.Fatalf("widgets above = %d children, want the default spacer", len(app.widgetAbove.Children))
	}
	if _, ok := app.widgetAbove.Children[0].(*tui.Spacer); !ok {
		t.Fatalf("child = %T, want Spacer", app.widgetAbove.Children[0])
	}
}

func outputPadTheme(t *testing.T) {
	t.Helper()
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	t.Cleanup(func() { SetRegisteredThemes(nil) })
}

// The branch and compaction summary blocks follow it too: at pad 1 the block's lines are
// indented, at pad 0 they are not.
func TestSummaryBlocksFollowOutputPad(t *testing.T) {
	outputPadTheme(t)

	indented := func(rendered []string) int {
		count := 0
		for _, line := range rendered {
			if strings.HasPrefix(coding.StripAnsi(line), " ") {
				count++
			}
		}
		return count
	}

	branch := NewBranchSummaryMessageComponent("branched here", nil, 0)
	unpadded := indented(branch.Render(40))
	branch.SetOutputPad(1)
	padded := indented(branch.Render(40))
	if padded <= unpadded {
		t.Fatalf("branch summary indented lines: %d at pad 0, %d at pad 1", unpadded, padded)
	}

	compaction := NewCompactionSummaryMessageComponent("compacted", 1000, nil, 0)
	unpaddedCompaction := indented(compaction.Render(40))
	compaction.SetOutputPad(1)
	paddedCompaction := indented(compaction.Render(40))
	if paddedCompaction <= unpaddedCompaction {
		t.Fatalf("compaction summary indented lines: %d at pad 0, %d at pad 1", unpaddedCompaction, paddedCompaction)
	}
}

// outputPad reaches tool output: the content box follows the setting instead of the hardcoded
// padding it carried before (upstream #10557, which extended outputPad from chat messages to
// every transcript block).
func TestToolOutputFollowsOutputPad(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	t.Cleanup(func() { SetRegisteredThemes(nil) })

	definition := builtinToolRenderers["bash"]
	render := func(padding int) []string {
		component := NewToolExecutionComponent("bash", "call-1", map[string]any{"command": "ls"}, ToolExecutionOptions{}, &definition, nil, "/tmp")
		component.SetOutputPad(padding)
		component.MarkExecutionStarted()
		component.SetArgsComplete()
		component.UpdateResult(&SortToolResultContent{
			Content: []ToolResultContent{{Type: "text", Text: "one\ntwo"}},
		}, false)
		return component.Render(40)
	}

	padded := render(1)
	unpadded := render(0)
	if len(padded) != len(unpadded) {
		t.Fatalf("padding changed the line count: %d padded, %d unpadded", len(padded), len(unpadded))
	}
	indented := 0
	for index := range padded {
		withPad := coding.StripAnsi(padded[index])
		withoutPad := coding.StripAnsi(unpadded[index])
		if strings.TrimSpace(withPad) == strings.TrimSpace(withoutPad) {
			// The same text: the padded line must be indented further than the unpadded one.
			if strings.HasPrefix(withPad, " ") != strings.HasPrefix(withoutPad, " ") {
				indented++
			}
			continue
		}
		t.Fatalf("line %d differs beyond its padding: %q vs %q", index, withPad, withoutPad)
	}
	if indented == 0 {
		t.Fatal("tool output ignored outputPad: no line was indented")
	}

	// The content is still there, so the padding did not eat it.
	if !strings.Contains(coding.StripAnsi(strings.Join(unpadded, "\n")), "one") {
		t.Fatalf("unpadded render lost the output: %q", unpadded)
	}
}

// cacheShare has no upstream counterpart (D416), but every other one-decimal
// display in this module formats through ai.JSNumberToFixed, matching JS
// toFixed, which rounds halfway cases away from zero (pi v1.5.0 f9a26365
// modes/interactive/components/footer.ts:27-29, :162). Go's strconv.FormatFloat
// rounds them to even, so a ratio landing exactly on a half printed one tenth
// low (D435).
func TestCacheShareRoundsHalvesLikeToFixed(t *testing.T) {
	for _, tc := range []struct {
		in        int64
		cacheRead int64
		want      string
	}{
		// Exact halves: 1/16 is 6.25%, which toFixed(1) prints as 6.3.
		{16, 1, "CH6.3%"},
		{32, 2, "CH6.3%"},
		{80, 1, "CH1.3%"},
		{400, 1, "CH0.3%"},
		{1600, 100, "CH6.3%"},
		// Non-halfway cases keep their value.
		{1000, 927, "CH92.7%"},
		{3, 1, "CH33.3%"},
		{3, 2, "CH66.7%"},
		{8, 5, "CH62.5%"},
		// A provider reporting more cached than counted input clamps at 100.
		{1000, 1500, "CH100.0%"},
		{1, 2, "CH100.0%"},
		// No cache activity or no input reports nothing.
		{1000, 0, ""},
		{0, 0, ""},
	} {
		got := workingTokenCount{cacheRead: tc.cacheRead, in: tc.in}.cacheShare()
		if got != tc.want {
			t.Errorf("cacheShare(in=%d, cacheRead=%d) = %q, want %q", tc.in, tc.cacheRead, got, tc.want)
		}
	}
}

func TestConfigureLowBandwidthDoesNotEnableOverSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 55000")
	t.Setenv("PIER_LOW_BANDWIDTH", "1")
	defer tui.SetLowBandwidth(false)
	tui.SetLowBandwidth(true)
	if ConfigureLowBandwidth() {
		t.Fatal("low-bandwidth rendering enabled over SSH")
	}
	if tui.LowBandwidth() {
		t.Fatal("SSH boot left low-bandwidth renderer enabled")
	}
}

// TestToolExecutionComponentAnimatesWhileRunning pins the tool-level animation:
// a running shell tool reports a shellElapsedTick frame so the elapsed label
// repaints, every other running tool keeps the 1s frame, and a finished or
// not-yet-started tool does not.
func TestToolExecutionComponentAnimatesWhileRunning(t *testing.T) {
	component := &ToolExecutionComponent{}
	if needs, _ := component.AnimationFrame(time.Now()); needs {
		t.Fatal("a tool that has not started animates")
	}
	component.executionStarted = true
	component.isPartial = true
	needs, delay := component.AnimationFrame(time.Now())
	if !needs || delay != time.Second {
		t.Fatalf("running tool: needs=%v delay=%v", needs, delay)
	}

	shell := &ToolExecutionComponent{toolName: "bash", executionStarted: true, isPartial: true}
	if needs, delay := shell.AnimationFrame(time.Now()); !needs || delay != shellElapsedTick {
		t.Fatalf("running shell tool: needs=%v delay=%v, want %v", needs, delay, shellElapsedTick)
	}
	component.isPartial = false
	if needs, _ := component.AnimationFrame(time.Now()); needs {
		t.Fatal("a finished tool animates")
	}
}
