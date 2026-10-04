package tui

import "strings"

// Port of src/components/box.ts: a container that applies padding and an
// optional background to its children.

type boxRenderCache struct {
	// childLines holds each child's rendered lines as returned by the child (no
	// padding applied), so validating the cache does not build a padded copy of
	// every line.
	childLines [][]string
	// childVersions and childComponents mirror the children at the cached pass.
	// A child Container rebuilds its suffix in place (the same backing array), so
	// comparing the line slices alone cannot see the change; the revision can.
	childVersions   []uint64
	childComponents []Component
	paddingX        int
	width           int
	bgSample        string
	hasBgSample     bool
	lines           []string
}

// Box applies padding and a background to all children.
type Box struct {
	Children []Component

	paddingX int
	paddingY int
	bgFn     func(text string) string

	cache            *boxRenderCache
	mouseLayout      []mouseChild
	mouseLayoutWidth int

	// frame scratch, reused across renders (loop-owned).
	childLines   [][]string
	leftPadCache string
	// bgSample/bgSampleSet cache bgFn("test") — one allocation per box per
	// frame otherwise. SetBgFn clears them.
	bgSample    string
	bgSampleSet bool
}

// NewBox creates a box with the given padding and optional background.
func NewBox(paddingX int, paddingY int, bgFn func(text string) string) *Box {
	return &Box{paddingX: paddingX, paddingY: paddingY, bgFn: bgFn}
}

// AddChild appends a child.
func (b *Box) AddChild(component Component) {
	b.Children = append(b.Children, component)
	b.cache = nil
}

// RemoveChild removes a child.
func (b *Box) RemoveChild(component Component) {
	for i, child := range b.Children {
		if child == component {
			b.Children = append(b.Children[:i], b.Children[i+1:]...)
			b.cache = nil
			return
		}
	}
}

// Clear removes all children.
func (b *Box) Clear() {
	b.Children = nil
	b.cache = nil
}

// SetBgFn updates the background function. The cache is kept because the
// background change is detected by sampling the function output.
func (b *Box) SetBgFn(bgFn func(text string) string) {
	b.bgFn = bgFn
	b.bgSample = ""
	b.bgSampleSet = false
	b.cache = nil
}

// childComponents implements childrenHolder.
func (b *Box) childComponents() []Component { return b.Children }

// Invalidate drops the cache and invalidates the children.
func (b *Box) Invalidate() {
	b.cache = nil
	for _, child := range b.Children {
		child.Invalidate()
	}
}

func (b *Box) matchCache(width int, bgSample string, hasBgSample bool) bool {
	cache := b.cache
	if cache == nil {
		return false
	}
	if cache.width != width || cache.paddingX != b.paddingX ||
		cache.hasBgSample != hasBgSample || cache.bgSample != bgSample {
		return false
	}
	if len(cache.childLines) != len(b.childLines) {
		return false
	}
	for i, cached := range cache.childLines {
		child := b.Children[i]
		if i >= len(cache.childComponents) || cache.childComponents[i] != child {
			return false
		}
		if versioned, ok := child.(renderVersioner); ok {
			if version, has := versioned.RenderVersion(); has {
				if i >= len(cache.childVersions) || cache.childVersions[i] != version {
					return false
				}
				// The revision covers the child's lines.
				continue
			}
		}
		lines := b.childLines[i]
		if len(cached) != len(lines) {
			return false
		}
		for j, line := range lines {
			if cached[j] != line {
				return false
			}
		}
	}
	return true
}

// firstChangedChildLine returns the flattened index of the first child line
// whose text differs from the cached pass, or -1 when every line matches. It is
// only meaningful when the cache geometry (width, padding, background, child
// count) already matches; Render guards it before reusing the backgrounded
// prefix.
func (b *Box) firstChangedChildLine() int {
	global := 0
	for i, cached := range b.cache.childLines {
		lines := b.childLines[i]
		common := min(len(cached), len(lines))
		for j := 0; j < common; j++ {
			if cached[j] != lines[j] {
				return global + j
			}
		}
		if len(cached) != len(lines) {
			// Lines appended or dropped at the end: the common prefix still
			// matches, so the first change starts after it.
			return global + common
		}
		global += len(lines)
	}
	return -1
}

// HandleMouse forwards an event to the child under the pointer.
func (b *Box) HandleMouse(event TuiMouseEvent) *TuiMouseDispatchResult {
	contentWidth := max(1, event.Width-b.paddingX*2)
	contentY := event.Y - b.paddingY
	contentX := event.X - b.paddingX
	if contentY < 0 || contentX < 0 || contentX >= contentWidth {
		return nil
	}

	mouseChildren := b.mouseLayout
	if b.mouseLayoutWidth != contentWidth {
		mouseChildren = make([]mouseChild, 0, len(b.Children))
		for _, child := range b.Children {
			mouseChildren = append(mouseChildren, mouseChild{component: child, height: len(child.Render(contentWidth))})
		}
	}
	childY := 0
	for _, child := range mouseChildren {
		if contentY >= childY && contentY < childY+child.height {
			childEvent := event
			childEvent.X = contentX
			childEvent.Y = contentY - childY
			childEvent.Width = contentWidth
			childEvent.Height = child.height
			return DispatchMouseEvent(child.component, childEvent)
		}
		childY += child.height
	}
	return nil
}

// Render renders the children with padding and background.
func (b *Box) Render(width int) []string {
	if len(b.Children) == 0 {
		return nil
	}

	contentWidth := max(1, width-b.paddingX*2)
	leftPad := ""
	if b.paddingX > 0 {
		if len(b.leftPadCache) != b.paddingX {
			b.leftPadCache = strings.Repeat(" ", b.paddingX)
		}
		leftPad = b.leftPadCache
	}

	if cap(b.childLines) < len(b.Children) {
		b.childLines = make([][]string, 0, len(b.Children))
	} else {
		b.childLines = b.childLines[:0]
	}
	if cap(b.mouseLayout) < len(b.Children) {
		b.mouseLayout = make([]mouseChild, 0, len(b.Children))
	} else {
		b.mouseLayout = b.mouseLayout[:0]
	}
	totalLines := 0
	for _, child := range b.Children {
		lines := child.Render(contentWidth)
		b.childLines = append(b.childLines, lines)
		b.mouseLayout = append(b.mouseLayout, mouseChild{component: child, height: len(lines)})
		totalLines += len(lines)
	}
	b.mouseLayoutWidth = contentWidth

	if totalLines == 0 {
		return nil
	}

	bgSample := ""
	hasBgSample := false
	if b.bgFn != nil {
		if !b.bgSampleSet {
			b.bgSample = b.bgFn("test")
			b.bgSampleSet = true
		}
		bgSample = b.bgSample
		hasBgSample = true
	}

	if b.matchCache(width, bgSample, hasBgSample) {
		return b.cache.lines
	}

	// Reuse the backgrounded prefix when only a suffix changed. A running tool's
	// elapsed label changes one trailing line, and re-applying the background
	// (VisibleWidth re-parses the ANSI) to every line of a long output was the
	// animation tick's remaining cost.
	firstChanged := -1
	if b.cache != nil && b.cache.width == width && b.cache.paddingX == b.paddingX &&
		b.cache.hasBgSample == hasBgSample && b.cache.bgSample == bgSample &&
		len(b.cache.childLines) == len(b.childLines) {
		firstChanged = b.firstChangedChildLine()
	}
	reuse := 0
	if firstChanged >= 0 {
		reuse = min(b.paddingY+firstChanged, len(b.cache.lines))
	}

	result := make([]string, 0, totalLines+b.paddingY*2)
	if reuse > 0 {
		result = append(result, b.cache.lines[:reuse]...)
	}
	for i := len(result); i < b.paddingY; i++ {
		result = append(result, b.applyBg("", width))
	}
	global := 0
	for _, lines := range b.childLines {
		for _, line := range lines {
			if global >= firstChanged || firstChanged < 0 {
				result = append(result, b.applyBg(leftPad+line, width))
			}
			global++
		}
	}
	for i := 0; i < b.paddingY; i++ {
		result = append(result, b.applyBg("", width))
	}

	childVersions := make([]uint64, len(b.Children))
	for i, child := range b.Children {
		if versioned, ok := child.(renderVersioner); ok {
			if version, has := versioned.RenderVersion(); has {
				childVersions[i] = version
			}
		}
	}
	b.cache = &boxRenderCache{
		childLines: append([][]string{}, b.childLines...), paddingX: b.paddingX,
		childVersions: childVersions, childComponents: append([]Component{}, b.Children...),
		width: width, bgSample: bgSample, hasBgSample: hasBgSample, lines: result,
	}
	return result
}

func (b *Box) applyBg(line string, width int) string {
	// VisibleWidth is computed here, so apply the background directly rather
	// than through ApplyBackgroundToLine, which would compute it again on the
	// already-padded line.
	padNeeded := max(0, width-VisibleWidth(line))
	padded := line + strings.Repeat(" ", padNeeded)
	if b.bgFn != nil {
		return b.bgFn(padded)
	}
	return padded
}

var _ Component = (*Box)(nil)
var _ MouseHandler = (*Box)(nil)
