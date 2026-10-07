package interactive

import (
	"context"
	"github.com/dat267/pier/tui"
)

// D200: workers keep the stable forwarding reference, but optional completions
// cannot mutate UI state after its mode lifetime ends. Embedding preserves
// Current so renderer-type queries still unwrap the active screen.
type modeUI struct {
	*tui.TuiReference
	ctx context.Context
}

func (ui *modeUI) Post(fn func()) {
	if ui.ctx.Err() != nil {
		return
	}
	ui.TuiReference.Post(func() {
		if ui.ctx.Err() == nil {
			fn()
		}
	})
}
