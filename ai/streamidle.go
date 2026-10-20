package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Port of the idle timeout upstream installs on its HTTP dispatcher: the
// settings-backed timeout is undici's headersTimeout (no response headers) and
// its bodyTimeout (no body bytes), both in core/http-dispatcher.ts (D254). Go's
// http.Transport cannot take either per request, so one timer is armed before
// the request is sent and re-armed by every body read (D40). Cancelling the
// request context is what aborts the in-flight read.
//
// The timer counts time blocked waiting for bytes, not wall-clock silence: a
// loaded machine, a GC pause or a slow consumer between reads cannot abort a
// connection whose bytes already arrived.

// idleTimeoutError reports the settings-backed idle timeout expiring before the
// response headers arrived or while the stream was silent.
type idleTimeoutError struct {
	timeout      time.Duration
	afterHeaders bool
}

func (e *idleTimeoutError) Error() string {
	if e.afterHeaders {
		return fmt.Sprintf("Provider stream idle for %dms (httpIdleTimeoutMs)", e.timeout.Milliseconds())
	}
	return fmt.Sprintf("Provider response headers idle for %dms (httpIdleTimeoutMs)", e.timeout.Milliseconds())
}

// idleStreamBody re-arms the idle timer on every read and reports the timeout as
// its own error instead of the read error the cancellation produces.
type idleStreamBody struct {
	rc         io.ReadCloser
	parent     context.Context
	cancel     context.CancelFunc
	timeout    time.Duration
	timer      *time.Timer
	gotHeaders atomic.Bool
	blocked    atomic.Bool
	fired      atomic.Bool
}

func (b *idleStreamBody) Read(p []byte) (int, error) {
	if b.timer != nil {
		b.timer.Reset(b.timeout)
	}
	b.blocked.Store(true)
	n, err := b.rc.Read(p)
	b.blocked.Store(false)
	if err != nil && b.fired.Load() && ctxErr(b.parent) == nil {
		return n, b.idleError()
	}
	if n > 0 {
		// The window runs from the last delivered bytes, so a consumer that is
		// slow to ask for more is not mistaken for a silent connection.
		b.timer.Reset(b.timeout)
	}
	return n, err
}

func (b *idleStreamBody) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}
	b.cancel()
	return b.rc.Close()
}

func (b *idleStreamBody) idleError() error {
	return &idleTimeoutError{timeout: b.timeout, afterHeaders: b.gotHeaders.Load()}
}

// newIdleBody arms the idle timer shared by every guarded request.
func newIdleBody(parent context.Context, cancel context.CancelFunc, timeout time.Duration) *idleStreamBody {
	body := &idleStreamBody{parent: parent, cancel: cancel, timeout: timeout}
	body.timer = time.AfterFunc(timeout, body.fire)
	return body
}

func (b *idleStreamBody) fire() {
	// A cancelled parent is a user abort, not an idle stream.
	if ctxErr(b.parent) != nil {
		return
	}
	if b.gotHeaders.Load() && !b.blocked.Load() {
		// Nobody is waiting on the socket: keep waiting for the consumer.
		b.timer.Reset(b.timeout)
		return
	}
	b.fired.Store(true)
	b.cancel()
}

// guardIdleBody bounds the body reads of an already-received response, for the
// provider whose headers timeout is provider-shaped (Mistral). Closing the
// returned body releases the request context in every case.
func guardIdleBody(parent context.Context, cancel context.CancelFunc, body io.ReadCloser, timeoutMS *int) io.ReadCloser {
	if timeoutMS == nil || *timeoutMS <= 0 {
		return &cancelOnCloseBody{ReadCloser: body, cancel: cancel}
	}
	guarded := newIdleBody(parent, cancel, time.Duration(*timeoutMS)*time.Millisecond)
	guarded.gotHeaders.Store(true)
	guarded.rc = body
	return guarded
}

// cancelOnCloseBody releases a request context when the caller is done with the
// response.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// DoProviderRequest issues a provider request bounded by the settings-backed idle
// timeout. A nil or non-positive timeout sends the request unbounded, as before.
// The returned body must be closed; closing it also releases the request context.
func DoProviderRequest(req *http.Request, timeoutMS *int) (*http.Response, error) {
	if timeoutMS == nil || *timeoutMS <= 0 {
		return http.DefaultClient.Do(req)
	}
	parent := req.Context()
	ctx, cancel := context.WithCancel(parent)
	body := newIdleBody(parent, cancel, time.Duration(*timeoutMS)*time.Millisecond)
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		body.timer.Stop()
		cancel()
		if body.fired.Load() && ctxErr(parent) == nil {
			return nil, body.idleError()
		}
		return nil, err
	}
	body.gotHeaders.Store(true)
	body.rc = resp.Body
	resp.Body = body
	return resp, nil
}
