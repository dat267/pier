package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// OAuthCallback is the browser-redirect payload (upstream OAuthCallback).
type OAuthCallback struct {
	Code  string
	State string
	// Iss is the `iss` parameter of the authorization response (RFC 9207),
	// when present.
	Iss *string
}

// OAuthCallbackPage is the outcome shown on the browser page (upstream
// OAuthCallbackPage).
type OAuthCallbackPage struct {
	OK      bool
	Message string
	Details *string
}

// OAuthCallbackServerOptions configure the local redirect listener (upstream
// OAuthCallbackServerOptions).
type OAuthCallbackServerOptions struct {
	// Host listens on this address. Default: 127.0.0.1.
	Host string
	// RedirectHost is the host name in redirectUrl, for example `localhost`
	// for a client registered with it while listening on 127.0.0.1.
	// Default: Host.
	RedirectHost string
	Port         int
	Path         string
	// TimeoutMs bounds one pending callback. Default: 5 minutes.
	TimeoutMs int64
	// RenderPage renders the browser page as HTML; default is plain text.
	RenderPage func(page OAuthCallbackPage) string
}

// OAuthCallbackServer listens for the authorization redirect (upstream
// OAuthCallbackServer).
type OAuthCallbackServer struct {
	RedirectURL string
	server      *http.Server
	path        string
	timeoutMs   int64
	renderPage  func(OAuthCallbackPage) string

	mu      sync.Mutex
	pending map[string]*pendingCallback
}

type pendingCallback struct {
	ch    chan callbackResult
	timer *time.Timer
}

type callbackResult struct {
	callback OAuthCallback
	err      error
}

// ListenCallbackServer starts the local redirect listener (upstream
// OAuthCallbackServer.listen).
func ListenCallbackServer(ctx context.Context, options OAuthCallbackServerOptions) (*OAuthCallbackServer, error) {
	host := options.Host
	if host == "" {
		host = "127.0.0.1"
	}
	redirectHost := options.RedirectHost
	if redirectHost == "" {
		redirectHost = host
	}
	path := options.Path
	if path == "" {
		path = "/callback"
	}
	timeoutMs := options.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 5 * 60000
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, options.Port))
	if err != nil {
		return nil, err
	}
	instance := &OAuthCallbackServer{
		path:       path,
		timeoutMs:  timeoutMs,
		renderPage: options.RenderPage,
		pending:    map[string]*pendingCallback{},
	}
	instance.RedirectURL = fmt.Sprintf("http://%s:%d%s", bracketHost(redirectHost), listener.Addr().(*net.TCPAddr).Port, path)
	instance.server = &http.Server{Handler: http.HandlerFunc(instance.handle)}
	go func() {
		_ = instance.server.Serve(listener)
	}()
	return instance, nil
}

func bracketHost(host string) string {
	if host != "" && host[0] != '[' && host[len(host)-1] != ']' && hasColon(host) {
		return "[" + host + "]"
	}
	return host
}

func hasColon(host string) bool {
	for _, r := range host {
		if r == ':' {
			return true
		}
	}
	return false
}

// WaitForCallback waits for the redirect carrying this state (upstream
// waitForCallback).
func (s *OAuthCallbackServer) WaitForCallback(state string) (OAuthCallback, error) {
	s.mu.Lock()
	if _, exists := s.pending[state]; exists {
		s.mu.Unlock()
		return OAuthCallback{}, fmt.Errorf("OAuth state is already pending")
	}
	entry := &pendingCallback{ch: make(chan callbackResult, 1)}
	entry.timer = time.AfterFunc(time.Duration(s.timeoutMs)*time.Millisecond, func() {
		s.mu.Lock()
		delete(s.pending, state)
		s.mu.Unlock()
		entry.ch <- callbackResult{err: fmt.Errorf("OAuth callback timed out")}
	})
	s.pending[state] = entry
	s.mu.Unlock()
	result := <-entry.ch
	return result.callback, result.err
}

// Pending reports whether a waiter is registered for the state. Tests use it
// to drive the redirect only after WaitForCallback has registered.
func (s *OAuthCallbackServer) Pending(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[state]
	return ok
}

// Close rejects pending waits and stops the listener (upstream close).
func (s *OAuthCallbackServer) Close(ctx context.Context) error {
	s.mu.Lock()
	for state, entry := range s.pending {
		entry.timer.Stop()
		entry.ch <- callbackResult{err: fmt.Errorf("OAuth callback server closed")}
		delete(s.pending, state)
	}
	s.mu.Unlock()
	return s.server.Close()
}

// reply renders the browser page.
func (s *OAuthCallbackServer) reply(w http.ResponseWriter, status int, page OAuthCallbackPage) {
	if s.renderPage != nil {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.Header().Set("cache-control", "no-store")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(s.renderPage(page)))
		return
	}
	message := page.Message
	details := ""
	if page.Details != nil {
		details = *page.Details
	}
	text := page.Message
	if !page.OK {
		if details != "" {
			text = page.Message + "\n\n" + details
		}
	} else {
		text = "Authorization complete. You may close this window."
	}
	if message == "" && page.OK {
		text = "Authorization complete. You may close this window."
	}
	w.Header().Set("content-type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text))
}

// handle serves the redirect (upstream handle).
func (s *OAuthCallbackServer) handle(w http.ResponseWriter, r *http.Request) {
	parsed, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		s.reply(w, http.StatusBadRequest, OAuthCallbackPage{OK: false, Message: "Invalid or expired OAuth state"})
		return
	}
	if parsed.Path != s.path {
		s.reply(w, http.StatusNotFound, OAuthCallbackPage{OK: false, Message: "Not found"})
		return
	}
	state := parsed.Query().Get("state")
	s.mu.Lock()
	entry := s.pending[state]
	if state == "" || entry == nil {
		s.mu.Unlock()
		s.reply(w, http.StatusBadRequest, OAuthCallbackPage{OK: false, Message: "Invalid or expired OAuth state"})
		return
	}
	entry.timer.Stop()
	delete(s.pending, state)
	s.mu.Unlock()
	if failure := parsed.Query().Get("error"); failure != "" {
		description := parsed.Query().Get("error_description")
		if description == "" {
			description = failure
		}
		entry.ch <- callbackResult{err: fmt.Errorf("%s", description)}
		s.reply(w, http.StatusOK, OAuthCallbackPage{
			OK:      false,
			Message: "Authorization failed. You may close this window.",
			Details: &description,
		})
		return
	}
	code := parsed.Query().Get("code")
	if code == "" {
		entry.ch <- callbackResult{err: fmt.Errorf("OAuth callback did not include an authorization code")}
		s.reply(w, http.StatusBadRequest, OAuthCallbackPage{OK: false, Message: "Missing authorization code"})
		return
	}
	callback := OAuthCallback{Code: code, State: state}
	if iss := parsed.Query().Get("iss"); iss != "" {
		callback.Iss = &iss
	}
	entry.ch <- callbackResult{callback: callback}
	s.reply(w, http.StatusOK, OAuthCallbackPage{OK: true, Message: "Authorization complete. You may close this window."})
}
