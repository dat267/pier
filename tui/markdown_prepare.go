package tui

// MarkdownPreparation is an opt-in off-loop renderer. Submit must run work on
// a worker, then execute its returned completion on the component's owner loop.
// It returns false without retaining work when admission is full or closed.
type MarkdownPreparation struct {
	Submit        func(work func() func()) bool
	RequestRender func()
}

const largeMarkdownBytes = 64 * 1024

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
