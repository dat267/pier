package interactive

import (
	"context"

	"github.com/dat267/pier/internal/offloop"
)

// sessionFileWorkKey names the one slot the session-replacement operations
// share: a switch, fork, import or tail supersedes the one before it, so a late
// finish cannot apply over the session the user actually chose. A rename uses
// its own key: it replaces nothing, and cancelling it would leave the picker
// waiting for a callback that never comes.
const sessionFileWorkKey = "session-file"

// runSessionFileWork runs session-file work (a file parse, a fork write, an
// import copy) off the owner loop and delivers the result to it.
//
// Those operations take from tens to hundreds of milliseconds on a long session:
// measured on a 25,583-entry session, opening the file is 57ms, writing a fork
// 519ms and copying an import 22ms. Doing them in front of the UI froze it, so
// the file work goes to the queue and the UI rebind that follows (which owns
// attached state and must not run on a worker) is the callback.
//
// A nil queue or nil post keeps the work synchronous, which is the path tests
// and embedders without a loop use.
func runSessionFileWork[T any](queue *offloop.Queue, post func(func()), key string, work func(context.Context) (T, error), done func(T, error)) {
	if queue == nil || post == nil {
		value, err := work(context.Background())
		done(value, err)
		return
	}
	queue.GoLatestContext(key, func(ctx context.Context) {
		value, err := work(ctx)
		if ctx.Err() != nil {
			return
		}
		post(func() {
			// A superseded request must not apply: the user may have picked
			// another session, or quit, while this one was being read.
			if ctx.Err() != nil {
				return
			}
			done(value, err)
		})
	})
}

// postToOwnerLoop delivers fn to the owner loop. It runs inline when the app has
// no loop draining posts (a test app or an embedder), where it is also the only
// writer.
func (a *App) postToOwnerLoop(fn func()) {
	if a.postToUI == nil {
		fn()
		return
	}
	a.postToUI(fn)
}
