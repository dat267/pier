package interactive

import (
	"testing"

	"github.com/dat267/pier/tui"
)

// BenchmarkTranscriptFrameCold measures the forced path beside the warm one.
// RenderNow(true) resets the frame (what a streaming or resume paint does), so
// the two curves together say whether a 100-200 ms stall record can come from the
// change-detection walk (warm) or from re-rendering the tree (cold).
func BenchmarkTranscriptFrameCold(b *testing.B) {
	for _, messages := range []int{500, 2000, 8000} {
		b.Run(itoa(messages)+"msgs", func(b *testing.B) {
			app, cleanup := newTestAppB(b)
			defer cleanup()
			if screen, ok := app.initialUI.(*tui.AltScreen); ok {
				screen.Start()
				screen.DisableAutoRender()
			}
			buildScrollTranscriptN(b, app, messages)
			app.ui.RenderNow(true) // cold: everything re-renders
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				app.ui.RenderNow(true)
			}
		})
	}
}

// BenchmarkTranscriptFrameScaling measures the warm fullscreen frame cost at
// growing transcript sizes. A frame normally re-walks every mounted component
// to detect changes (each component then returns its cached lines, and Markdown
// reports 100% cache hits), so the cost is O(components), not O(visible lines).
// A Container whose children are all reusable now returns its cached frame
// without walking, snapshotting the child list, or rebuilding the mouse layout
// (Container.allChildrenReusable), and when it does walk it reads each child's
// revision once. The curve is ~50 us at 500 message pairs, ~150 us at 2,000 and
// ~1.4 ms at 8,000 (down from ~74 us, ~260 us and ~2.0 ms before the fast path).
// This pins the curve so a regression, or the global-revision follow-up
// described in docs/architecture.md, is visible.
func BenchmarkTranscriptFrameScaling(b *testing.B) {
	for _, messages := range []int{500, 2000, 8000} {
		b.Run(itoa(messages)+"msgs", func(b *testing.B) {
			app, cleanup := newTestAppB(b)
			defer cleanup()
			if screen, ok := app.initialUI.(*tui.AltScreen); ok {
				screen.Start()
				screen.DisableAutoRender()
			}
			buildScrollTranscriptN(b, app, messages)
			app.ui.RenderNow(false) // warm every cache
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				app.ui.RenderNow(false)
			}
		})
	}
}
