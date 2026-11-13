package ai

import (
	"net/http"
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
