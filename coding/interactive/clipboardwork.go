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

// copyToClipboardOffLoop is the CopyToClipboard seam of /copy and the tree
// selector. The clipboard subprocess can hang for up to its timeout (xclip
// serving a selection, a wedged clipboard daemon), so it runs off the loop; the
// caller confirms optimistically and failures are marshaled back through the
// error seam.
func (a *App) copyToClipboardOffLoop(text string) (bool, string) {
	accepted := a.tryCopyClipboard(text, func(err error) {
		if err == nil {
			return
		}
		a.ui.Post(func() { a.showError(err.Error()) })
	})
	if !accepted {
		return false, errClipboardBusy.Error()
	}
	return true, ""
}
