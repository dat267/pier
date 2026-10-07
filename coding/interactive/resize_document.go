package interactive

import "github.com/dat267/pier/tui"

// D203: resizing a settled transcript invalidates every width-shaped cache.
// Warm attached components cooperatively on the owner loop, never concurrently
// with their mutation. Until ready, keep the last complete flattened frame.
type resizeDocument struct {
	*tui.Container
	requestRender func()
	lines         []string
	width         int
	targetWidth   int
	children      []tui.Component
	next          int
}

func newResizeDocument(chat *tui.Container, requestRender func()) *resizeDocument {
	return &resizeDocument{Container: chat, requestRender: requestRender}
}

func (d *resizeDocument) Render(width int) []string {
	if d.lines != nil && width != d.width {
		if width != d.targetWidth || !d.sameChildren() {
			d.targetWidth = width
			d.children = append(d.children[:0], d.Children...)
			d.next = 0
		}
		end := min(d.next+64, len(d.children))
		for d.next < end {
			d.children[d.next].Render(width)
			d.next++
		}
		if d.next < len(d.children) {
			if d.requestRender != nil {
				d.requestRender()
			}
			return d.lines
		}
	}
	d.lines = d.Container.Render(width)
	d.width = width
	d.targetWidth = 0
	d.children = nil
	d.next = 0
	return d.lines
}

// RenderVersion disables ancestor cache skipping while a width generation is
// incomplete, so a requested continuation reaches Render on the next paint.
func (d *resizeDocument) RenderVersion() (uint64, bool) {
	if d.targetWidth != 0 {
		return 0, false
	}
	return d.Container.RenderVersion()
}

func (d *resizeDocument) sameChildren() bool {
	if len(d.children) != len(d.Children) {
		return false
	}
	for i, child := range d.children {
		if child != d.Children[i] {
			return false
		}
	}
	return true
}
