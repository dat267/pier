package interactive

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

func TestToolFramesRejectBusyWorkWithoutLosingDisplayedFrame(t *testing.T) {
	InitTheme("dark", false)
	theme := ActiveTheme().concrete()
	box := tui.NewBox(1, 1, func(text string) string { return theme.Bg("toolSuccessBg", text) })
	root := &tui.Container{}
	root.AddChild(tui.NewSpacer(1))
	root.AddChild(box)
	frames := newToolFrames(root, box)
	accept := true
	attempts := 0
	var work func() func()
	frames.SetPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool {
		attempts++
		if !accept {
			return false
		}
		work = run
		return true
	}})
	update := func(text string) {
		box.Clear()
		box.AddChild(tui.NewText("header", 0, 0, nil))
		box.AddChild(frames.Update(&toolFrameInput{build: func() tui.Component { return tui.NewText(text, 0, 0, nil) }, theme: theme, background: "toolSuccessBg", boxed: true}))
		root.MarkDirty()
	}
	update("old")
	frames.Render(80)
	work()()
	complete := append([]string(nil), frames.Render(80)...)
	accept = false
	update("new")
	if !reflect.DeepEqual(frames.Render(80), complete) {
		t.Fatal("rejection lost displayed frame")
	}
	if _, stable := frames.RenderVersion(); stable {
		t.Fatal("unadmitted work can be skipped by parent")
	}
	before := attempts
	accept = true
	frames.Render(80)
	if attempts != before+1 {
		t.Fatal("rejected work was not retried")
	}
	if _, stable := frames.RenderVersion(); !stable {
		t.Fatal("accepted work lacks owner revision")
	}
	work()()
	if !strings.Contains(strings.Join(frames.Render(80), "\n"), "new") {
		t.Fatal("retry completion did not publish")
	}
}

// The lifecycle seam is the owner-supplied preparation executor. Snapshot
// construction is separate; lifecycle tests need no ToolExecution internals.
func TestToolFramesPublishOnlyMatchingCompletedSnapshot(t *testing.T) {
	InitTheme("dark", false)
	theme := ActiveTheme().concrete()
	box := tui.NewBox(1, 1, func(text string) string { return theme.Bg("toolSuccessBg", text) })
	root := &tui.Container{}
	root.AddChild(tui.NewSpacer(1))
	root.AddChild(box)
	var jobs []func() func()
	frames := newToolFrames(root, box)
	frames.SetPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { jobs = append(jobs, run); return true }})
	update := func(text string) {
		box.Clear()
		box.AddChild(tui.NewText("read output.txt", 0, 0, nil))
		box.AddChild(frames.Update(&toolFrameInput{build: func() tui.Component { return tui.NewText(text, 0, 0, nil) }, theme: theme, background: "toolSuccessBg", boxed: true}))
		root.MarkDirty()
	}
	update("old output")
	pending := frames.Render(80)
	if len(jobs) != 1 {
		t.Fatal("lifecycle did not submit private work")
	}
	apply := jobs[0]()
	if !reflect.DeepEqual(frames.Render(80), pending) {
		t.Fatal("worker changed owner display")
	}
	apply()
	complete := append([]string(nil), frames.Render(80)...)
	update("new output")
	frames.Render(80)
	stale := jobs[1]()
	update("latest output")
	frames.Render(80)
	stale()
	if !reflect.DeepEqual(frames.Render(80), complete) {
		t.Fatal("stale completion replaced retained display")
	}
	jobs[2]()()
	if !strings.Contains(strings.Join(frames.Render(80), "\n"), "latest output") {
		t.Fatal("matching completion never published")
	}
}
