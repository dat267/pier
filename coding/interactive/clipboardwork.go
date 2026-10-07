package interactive

import (
	"context"
	"errors"
)

// D201: count running plus waiting copies; accepted copies stay FIFO.
const maxClipboardCopies = 4

var errClipboardBusy = errors.New("Clipboard busy; try again after pending copies finish.")

// D200: interactive copy paths share a mode-owned FIFO, not the legacy
// process-wide clipboard queue. Callers marshal completion through modeUI.
func (a *App) copyClipboardAsync(text string, onDone func(error)) {
	if !a.tryCopyClipboard(text, onDone) && a.optionalContext.Err() == nil && onDone != nil {
		onDone(errClipboardBusy)
	}
}

func (a *App) tryCopyClipboard(text string, onDone func(error)) bool {
	if a.optionalContext.Err() != nil {
		return false
	}
	return a.clipboardQueue.TryGoContext(maxClipboardCopies, func(ctx context.Context) {
		if ctx.Err() != nil || a.optionalContext.Err() != nil {
			return
		}
		err := a.copyText(ctx, text)
		if ctx.Err() == nil && a.optionalContext.Err() == nil && onDone != nil {
			onDone(err)
		}
	})
}
