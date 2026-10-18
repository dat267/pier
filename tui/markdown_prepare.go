package tui

// MarkdownPreparation is an opt-in off-loop renderer. Submit must run work on
// a worker, then execute its returned completion on the component's owner loop.
// It returns false without retaining work when admission is full or closed.
type MarkdownPreparation struct {
	Submit        func(work func() func()) bool
	RequestRender func()
}

// D239: 64 KiB left a streaming message re-wrapping its open paragraph on the
// owner loop for its whole life (one paint measured 94ms); 8 KiB keeps the
// first (synchronous) render cheap while deltas render off-loop.
const largeMarkdownBytes = 8 * 1024

// veryLargeMarkdownBytes is D204's cold-preparation threshold: a message this
// size submits off-loop on its first render (the owner never parses it),
// accepting the brief placeholder.
const veryLargeMarkdownBytes = 64 * 1024

// SetPreparation installs an immutable preparation backend before first use.
// Standalone Markdown stays synchronous unless its caller opts in.
func (m *Markdown) SetPreparation(preparation *MarkdownPreparation) {
	m.preparation = preparation
}

// prepareLarge snapshots configuration, never the attached component or its
// mutable caches. D204: stale content/width/style generations cannot publish.
func (m *Markdown) prepareLarge(width int) []string {
	if !m.preparing || m.preparingText != m.Text || m.preparingWidth != width {
		m.preparationGeneration++
		generation := m.preparationGeneration
		snapshot := NewMarkdown(m.Text, m.PaddingX, m.PaddingY, m.Theme, nil, m.Options)
		if m.DefaultTextStyle != nil {
			style := *m.DefaultTextStyle
			snapshot.DefaultTextStyle = &style
		}
		m.preparingText = m.Text
		m.preparingWidth = width
		m.preparing = true
		accepted := m.preparation.Submit(func() func() {
			snapshot.Render(width)
			return func() {
				if m.preparationGeneration != generation || m.Text != snapshot.Text {
					return
				}
				preparation := m.preparation
				*m = *snapshot
				m.preparation = preparation
				m.preparationGeneration = generation
				m.displayLines = m.cachedLines
				if preparation.RequestRender != nil {
					preparation.RequestRender()
				}
			}
		})
		if !accepted {
			m.preparing = false
			if m.preparation.RequestRender != nil {
				m.preparation.RequestRender()
			}
		}
	}
	if m.displayLines != nil {
		return m.displayLines
	}
	return []string{"Preparing message..."}
}
