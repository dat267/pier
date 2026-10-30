package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Port of the idle timeout upstream installs on its HTTP dispatcher:
// core/http-dispatcher.ts sets Undici headersTimeout and bodyTimeout (D254).
// Go's http.Transport cannot take either per request, so one timer is armed
// before the request and re-armed by each body read (D40). Dispatcher-only
// requests use the raw FastTimer delay in both phases. SDK-backed OpenAI and
// Anthropic requests also use Node setTimeout while waiting for headers, then
// switch to the raw Undici body delay after headers arrive.
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
	rc            io.ReadCloser
	parent        context.Context
	cancel        context.CancelFunc
	headerTimeout time.Duration
	timeout       time.Duration
	timer         *time.Timer
	gotHeaders    atomic.Bool
	blocked       atomic.Bool
	fired         atomic.Bool
	closed        atomic.Bool
}

func (b *idleStreamBody) Read(p []byte) (int, error) {
	b.resetTimer()
	b.blocked.Store(true)
	n, err := b.rc.Read(p)
	b.blocked.Store(false)
	if err != nil && b.fired.Load() && ctxErr(b.parent) == nil {
		return n, b.idleError()
	}
	if n > 0 {
		// The window runs from the last delivered bytes, so a consumer that is
		// slow to ask for more is not mistaken for a silent connection.
		b.resetTimer()
	}
	return n, err
}

func (b *idleStreamBody) Close() error {
	b.closed.Store(true)
	if b.timer != nil {
		b.timer.Stop()
	}
	b.cancel()
	return b.rc.Close()
}

func (b *idleStreamBody) resetTimer() {
	if b.timer == nil || b.closed.Load() {
		return
	}
	if b.timeout <= 0 {
		b.timer.Stop()
		return
	}
	b.timer.Reset(b.timeout)
	// Close can stop the timer between the first check and Reset. Stop it
	// again if that happened, including resets from an in-flight callback.
	if b.closed.Load() {
		b.timer.Stop()
	}
}

func (b *idleStreamBody) idleError() error {
	afterHeaders := b.gotHeaders.Load()
	timeout := b.timeout
	if !afterHeaders {
		timeout = b.headerTimeout
	}
	return &idleTimeoutError{timeout: timeout, afterHeaders: afterHeaders}
}

// newIdleBody arms the idle timer shared by every guarded request.
func newIdleBody(parent context.Context, cancel context.CancelFunc, timeout time.Duration) *idleStreamBody {
	return newIdleBodyWithTimeouts(parent, cancel, timeout, timeout)
}

func newIdleBodyWithTimeouts(parent context.Context, cancel context.CancelFunc, headerTimeout, bodyTimeout time.Duration) *idleStreamBody {
	body := &idleStreamBody{parent: parent, cancel: cancel, headerTimeout: headerTimeout, timeout: bodyTimeout}
	if headerTimeout > 0 {
		body.timer = time.AfterFunc(headerTimeout, body.fire)
	}
	return body
}

// idleTimeoutDuration preserves Undici FastTimer millisecond delays.
// D305: saturate only when the delay cannot fit in Go's int64 nanoseconds.
func idleTimeoutDuration(timeoutMS int) time.Duration {
	const maxTimeoutMS = int64(1<<63-1) / int64(time.Millisecond)
	if int64(timeoutMS) > maxTimeoutMS {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func (b *idleStreamBody) fire() {
	if b.closed.Load() {
		return
	}
	// A cancelled parent is a user abort, not an idle stream.
	if ctxErr(b.parent) != nil {
		return
	}
	if b.gotHeaders.Load() && !b.blocked.Load() {
		// Nobody is waiting on the socket: keep waiting for the consumer.
		b.resetTimer()
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
	guarded := newIdleBody(parent, cancel, idleTimeoutDuration(*timeoutMS))
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

// doFetchRequest retains the configured client while applying fetch's redirect
// credential rule. Go otherwise trusts same-host port changes and subdomains.
// D284: retain configured redirect hooks and the existing ten-hop Go limit.
func doFetchRequest(req *http.Request) (*http.Response, error) {
	client := *http.DefaultClient
	checkRedirect := client.CheckRedirect
	crossedOrigin := false
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		crossedOrigin = crossedOrigin || requestOrigin(next.URL) != requestOrigin(via[len(via)-1].URL)
		var err error
		if checkRedirect != nil {
			err = checkRedirect(next, via)
		} else if len(via) >= 10 {
			err = fmt.Errorf("stopped after 10 redirects")
		}
		// Go can copy headers from the initial request again on later hops.
		// Once fetch strips credentials, they stay stripped for the chain.
		if crossedOrigin {
			for name := range next.Header {
				switch strings.ToLower(name) {
				case "authorization", "cookie", "proxy-authorization":
					delete(next.Header, name)
				}
			}
		}
		return err
	}
	return client.Do(req)
}

func requestOrigin(u *url.URL) [3]string {
	scheme, port := strings.ToLower(u.Scheme), u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return [3]string{scheme, strings.ToLower(u.Hostname()), port}
}

// DoProviderRequest applies the settings-backed Undici idle timeout to both
// response headers and body reads. A nil or non-positive timeout is unbounded.
// The returned body must be closed to release its request context.
func DoProviderRequest(req *http.Request, timeoutMS *int) (*http.Response, error) {
	return DoProviderRequestWithIdleTimeout(req, timeoutMS, nil)
}

// DoProviderRequestWithIdleTimeout keeps the provider request timeout and the
// global dispatcher idle timeout separate. With no explicit dispatcher value,
// timeoutMS remains the fallback for direct callers predating settings wiring.
func DoProviderRequestWithIdleTimeout(req *http.Request, timeoutMS, idleTimeoutMS *int) (*http.Response, error) {
	idleTimeoutMS = firstTimeout(idleTimeoutMS, timeoutMS)
	if idleTimeoutMS == nil || *idleTimeoutMS <= 0 {
		return doFetchRequest(req)
	}
	timeout := idleTimeoutDuration(*idleTimeoutMS)
	return doProviderRequestWithTimeouts(req, timeout, timeout)
}

// DoProviderRequestWithSDKTimeout applies Node's setTimeout-backed provider SDK
// deadline while awaiting headers. Undici's body idle timeout stays separate.
func DoProviderRequestWithSDKTimeout(req *http.Request, timeoutMS *int) (*http.Response, error) {
	return DoProviderRequestWithSDKTimeoutAndIdle(req, timeoutMS, nil)
}

// DoProviderRequestWithSDKTimeoutAndIdle applies the SDK header timeout and the
// independent Undici dispatcher idle timeout.
func DoProviderRequestWithSDKTimeoutAndIdle(req *http.Request, timeoutMS, idleTimeoutMS *int) (*http.Response, error) {
	idleTimeoutMS = firstTimeout(idleTimeoutMS, timeoutMS)
	var headerTimeout, bodyTimeout time.Duration
	if idleTimeoutMS != nil && *idleTimeoutMS > 0 {
		bodyTimeout = idleTimeoutDuration(*idleTimeoutMS)
		headerTimeout = bodyTimeout
	}
	if timeoutMS != nil && *timeoutMS > 0 {
		sdkHeaderTimeout := javascriptTimeoutDuration(int64(*timeoutMS))
		if headerTimeout == 0 || sdkHeaderTimeout < headerTimeout {
			headerTimeout = sdkHeaderTimeout
		}
	}
	if headerTimeout == 0 && bodyTimeout == 0 {
		return doFetchRequest(req)
	}
	return doProviderRequestWithTimeouts(req, headerTimeout, bodyTimeout)
}

// DoProviderRequestWithAbortSignalTimeout mirrors the Codex fetch deadline.
func DoProviderRequestWithAbortSignalTimeout(req *http.Request, timeoutMS *int) (*http.Response, error) {
	return DoProviderRequestWithAbortSignalTimeoutAndIdle(req, timeoutMS, nil)
}

// DoProviderRequestWithAbortSignalTimeoutAndIdle mirrors Codex's AbortSignal
// header deadline and the independent Undici dispatcher idle timeout.
func DoProviderRequestWithAbortSignalTimeoutAndIdle(req *http.Request, timeoutMS, idleTimeoutMS *int) (*http.Response, error) {
	if timeoutMS != nil && *timeoutMS < 0 {
		return nil, fmt.Errorf("Invalid timeoutMs: %d", *timeoutMS)
	}
	if timeoutMS != nil && int64(*timeoutMS) > maxAbortSignalTimeoutMS {
		return nil, fmt.Errorf("The value of \"delay\" is out of range. It must be >= 0 && <= %d. Received %d", maxAbortSignalTimeoutMS, *timeoutMS)
	}
	return DoProviderRequestWithSDKTimeoutAndIdle(req, timeoutMS, idleTimeoutMS)
}

func firstTimeout(primary, fallback *int) *int {
	if primary != nil {
		return primary
	}
	return fallback
}

func doProviderRequestWithTimeouts(req *http.Request, headerTimeout, bodyTimeout time.Duration) (*http.Response, error) {
	parent := req.Context()
	ctx, cancel := context.WithCancel(parent)
	body := newIdleBodyWithTimeouts(parent, cancel, headerTimeout, bodyTimeout)
	resp, err := doFetchRequest(req.WithContext(ctx))
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
	body.resetTimer()
	resp.Body = body
	return resp, nil
}

// defaultBoundedRequestTimeout bounds one non-streaming provider, auth or
// config request that does not carry its own deadline. Upstream installs its
// idle timeout on every fetch through the dispatcher, so a login or refresh
// cannot sit on a half-open connection forever (D255). A caller deadline is
// always shorter and therefore wins.
var defaultBoundedRequestTimeout = 60 * time.Second

// doBoundedRequest issues one non-streaming request under
// defaultBoundedRequestTimeout and keeps the timer alive for the body read:
// closing the response releases it.
func doBoundedRequest(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(req.Context(), defaultBoundedRequestTimeout)
	resp, err := doFetchRequest(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}
