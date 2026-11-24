package ai

import (
	"net/http"
	"sync"
	"time"
)

// oauthCallbackReadHeaderTimeout bounds how long a callback connection may take
// to send its request line and headers. These servers listen on loopback, so the
// peer is a local process: without the bound it can connect and stall, holding a
// handler goroutine and a descriptor for as long as it likes (gosec G112). The
// pin runs these servers on Node, whose headersTimeout default is 60s, while a
// zero value in Go means no bound at all. A var so a test can shorten it.
var oauthCallbackReadHeaderTimeout = 60 * time.Second

// newOAuthCallbackHTTPServer builds one of the loopback callback servers with
// that bound in place.
func newOAuthCallbackHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: oauthCallbackReadHeaderTimeout}
}

// oauthCallbackOutcome is the settle-once handoff a loopback callback server
// hands its waiter: the first of a result, a failure or a manual-entry cancel
// wins, and Wait reports it. The ChatGPT and Codex servers embed it.
type oauthCallbackOutcome[T any] struct {
	mu      sync.Mutex
	settled bool
	lastErr error
	result  chan T
	failure chan error
	done    chan struct{}
}

func newOAuthCallbackOutcome[T any]() oauthCallbackOutcome[T] {
	return oauthCallbackOutcome[T]{
		result:  make(chan T, 1),
		failure: make(chan error, 1),
		done:    make(chan struct{}),
	}
}

// claim marks the outcome settled, reporting false when it already was.
func (o *oauthCallbackOutcome[T]) claim() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.settled {
		return false
	}
	o.settled = true
	return true
}

func (o *oauthCallbackOutcome[T]) isSettled() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.settled
}

func (o *oauthCallbackOutcome[T]) settle(value T) {
	if o.claim() {
		o.result <- value
		close(o.done)
	}
}

func (o *oauthCallbackOutcome[T]) fail(err error) {
	if o.claim() {
		o.failure <- err
		close(o.done)
	}
}

// CancelWait hands the login over to manual code entry.
func (o *oauthCallbackOutcome[T]) CancelWait() {
	if o.claim() {
		close(o.done)
	}
}

// wait blocks until the outcome settles and returns the result, or ok=false
// after a failure (kept for LastError) or a cancel.
func (o *oauthCallbackOutcome[T]) wait() (T, bool) {
	<-o.done
	var zero T
	select {
	case value := <-o.result:
		return value, true
	case err := <-o.failure:
		o.mu.Lock()
		o.lastErr = err
		o.mu.Unlock()
		return zero, false
	default:
		return zero, false
	}
}

// LastError returns the callback failure, when the wait ended because of one.
func (o *oauthCallbackOutcome[T]) LastError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastErr
}
