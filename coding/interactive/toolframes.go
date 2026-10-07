package interactive

import (
	"slices"

	"github.com/dat267/pier/tui"
)

// toolFrames is the owner-only deep module for preparation and publication.
// Callers supply captured input and delegate rendering, revisions and hit tests.
// The submission executor is the seam; workers never inspect attached state.
// D205-D208: generations, cache adoption and retained clocks form one lifecycle.
type toolFrames struct {
	root        *tui.Container
	box         *tui.Box
	backend     *tui.MarkdownPreparation
	input       *toolFrameInput
	body        toolFrameBody
	generation  uint64
	width       int
	outerWidth  int
	pending     bool
	ready       bool
	raw         []string
	revision    uint64
	prepared    *toolFrameSnapshot
	displayed   *toolFrameSnapshot
	lines       []string
	retained    bool
	changedFrom int
}

type toolFrameInput struct {
	build      func() tui.Component
	theme      *Theme
	background string
	boxed      bool
	tail       *shellElapsedComponent // owner-only; workers receive captured text
}

type toolFrameSnapshot struct {
	box   *tui.Box
	width int
	tail  []string
	bg    func(string) string
}

type preparedFrameLines []string

func (lines preparedFrameLines) Render(int) []string { return lines }
func (preparedFrameLines) Invalidate()               {}

// toolFrameBody is only the private rendering adapter for the result slot.
// All lifecycle state belongs to toolFrames, not this adapter or its callers.
type toolFrameBody struct{ owner *toolFrames }

func (body *toolFrameBody) Invalidate() { body.owner.Invalidate() }
func (body *toolFrameBody) Render(width int) []string {
	f := body.owner
	lines := f.prepare(width)
	if f.input.tail == nil {
		return lines
	}
	return append(slices.Clone(lines), f.input.tail.Render(width)...)
}
func (body *toolFrameBody) RenderVersion() (uint64, bool) {
	f := body.owner
	return f.revision, f.input.tail == nil || f.input.tail.state.endedAtMS != 0
}

func newToolFrames(root *tui.Container, box *tui.Box) *toolFrames {
	f := &toolFrames{root: root, box: box}
	f.body.owner = f
	return f
}
func (f *toolFrames) Enabled() bool { return f.backend != nil }
func (f *toolFrames) SetPreparation(backend *tui.MarkdownPreparation) bool {
	rebuild := f.input != nil
	f.backend = backend
	f.Invalidate()
	return rebuild
}
func (f *toolFrames) Update(input *toolFrameInput) tui.Component {
	f.Invalidate()
	if f.backend == nil {
		input = nil
	}
	f.input = input
	if input == nil {
		f.raw = nil
		f.prepared = nil
		f.displayed = nil
		f.lines = nil
		return nil
	}
	return &f.body
}
func (f *toolFrames) Invalidate() { f.generation++; f.pending = false; f.ready = false; f.revision++ }
func (f *toolFrames) Prepare(width int, rebuild func()) {
	backend := f.backend
	f.backend = nil
	defer func() { f.backend = backend }()
	rebuild()
	f.Render(width)
}

func (f *toolFrames) Render(width int) []string {
	f.outerWidth = width
	f.retained = false
	f.changedFrom = -1
	if f.input != nil && f.input.boxed && f.displayed != nil {
		f.prepare(max(1, width-2))
		if !f.ready {
			f.retained = true
			return f.refreshClock()
		}
	}
	lines := f.root.Render(width)
	if f.input != nil && f.ready {
		f.displayed = f.prepared
		f.lines = lines
	}
	return lines
}
func (f *toolFrames) RenderVersion() (uint64, bool) {
	if f.input != nil && !f.ready && !f.pending && f.input.boxed && f.displayed != nil {
		return 0, false
	}
	return f.root.RenderVersion()
}
func (f *toolFrames) ChangedFrom() (int, bool) {
	if f.retained {
		return f.changedFrom, true
	}
	return f.root.ChangedFrom()
}
func (f *toolFrames) HandleMouse(event tui.TuiMouseEvent) *tui.TuiMouseDispatchResult {
	if f.retained && f.displayed != nil {
		event.Width = f.displayed.width
	}
	return f.root.HandleMouse(event)
}

func (f *toolFrames) notify() {
	f.root.MarkDirty()
	if f.backend != nil && f.backend.RequestRender != nil {
		f.backend.RequestRender()
	}
}
func (f *toolFrames) prepare(width int) []string {
	if f.width != width {
		f.Invalidate()
		f.width = width
	}
	if f.ready {
		return f.raw
	}
	if !f.pending {
		generation := f.generation
		build := f.input.build
		frame := f.capture(width)
		f.pending = true
		if !f.backend.Submit(func() func() {
			lines := build().Render(width)
			if frame != nil {
				body := lines
				if len(frame.tail) > 0 {
					body = append(slices.Clone(lines), frame.tail...)
				}
				frame.box.AddChild(preparedFrameLines(body))
				frame.box.Render(frame.width)
			}
			return func() {
				if f.generation != generation {
					return
				}
				f.raw = lines
				f.ready = true
				f.pending = false
				f.revision++
				f.prepared = nil
				if frame != nil && f.box.AdoptPreparedFrame(frame.box) {
					f.prepared = frame
				}
				f.notify()
			}
		}) {
			f.pending = false
			f.notify()
		}
	}
	if f.raw != nil {
		return f.raw
	}
	return []string{"Preparing tool output..."}
}
func (f *toolFrames) capture(width int) *toolFrameSnapshot {
	input := f.input
	if !input.boxed || len(f.box.Children) != 2 {
		return nil
	}
	header := slices.Clone(f.box.Children[0].Render(width))
	theme, background := input.theme, input.background
	bg := func(text string) string { return theme.Bg(background, text) }
	box := tui.NewBox(1, 1, bg)
	box.AddChild(preparedFrameLines(header))
	frame := &toolFrameSnapshot{box: box, width: f.outerWidth, bg: bg}
	if input.tail != nil {
		frame.tail = slices.Clone(input.tail.Render(width))
	}
	return frame
}
func (f *toolFrames) refreshClock() []string {
	frame := f.displayed
	if f.input.tail == nil || len(frame.tail) == 0 {
		return f.lines
	}
	tail := f.input.tail.Render(max(1, frame.width-2))
	if len(tail) != len(frame.tail) {
		return f.lines
	}
	start := len(f.lines) - 1 - len(tail)
	if start < 0 {
		return f.lines
	}
	for offset, line := range tail {
		styled := tui.ApplyBackgroundToLine(" "+line, frame.width, frame.bg)
		index := start + offset
		if f.lines[index] == styled {
			continue
		}
		if f.changedFrom < 0 {
			if f.pending {
				f.root.BumpRevision()
			} else {
				f.lines = slices.Clone(f.lines)
			}
			f.changedFrom = index
		}
		f.lines[index] = styled
	}
	return f.lines
}
