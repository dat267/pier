package coding

import (
	"fmt"
	"os"
	"strings"
)

// Port of core/http-dispatcher.ts. The undici-specific dispatcher (connection
// pooling, H2, CONNECT tunnels, the auto-select-family connect tuning) has no Go
// counterpart: Go's transport already pools connections and reads the proxy
// environment, and its dialer handles connect tuning internally.
//
// D40: upstream's dispatcher sets both headersTimeout and bodyTimeout to the idle
// timeout and can be reconfigured at runtime. Go's http.Transport exposes
// ResponseHeaderTimeout (the headersTimeout equivalent) and IdleConnTimeout, and
// there is no per-read body timeout, so the configured timeout reaches the wire
// as a per-request timer instead (ai/streamidle.go, D254): every streamed
// provider request arms it before sending and re-arms it on each body read, so
// silence before the headers or between chunks cancels the request context while
// an active stream outlives it. Mistral keeps upstream's headers-only scope.
//
// There is deliberately no function here that installs the timeout into the
// shared transport. http.DefaultTransport is process-wide and is read by net/http
// while requests are in flight — its HTTP/2 transport reads ResponseHeaderTimeout
// per stream — so writing those fields on a settings change is a data race
// (go test -race flags it against an in-flight request; the idled keep-alive
// goroutines of an earlier request are enough). Upstream can reconfigure its
// dispatcher because its runtime is single-threaded; a Go port cannot, which is
// why this port applies the timeout per request instead.

// HTTPIdleTimeoutChoice is one selectable idle timeout.
type HTTPIdleTimeoutChoice struct {
	Label     string
	TimeoutMS int64
}

// HTTPIdleTimeoutChoices are the offered idle timeouts.
var HTTPIdleTimeoutChoices = []HTTPIdleTimeoutChoice{
	{Label: "30 sec", TimeoutMS: 30_000},
	{Label: "1 min", TimeoutMS: 60_000},
	{Label: "2 min", TimeoutMS: 120_000},
	{Label: "5 min", TimeoutMS: 300_000},
	{Label: "disabled", TimeoutMS: 0},
}

// FormatHTTPIdleTimeoutMS renders a timeout with its choice label.
func FormatHTTPIdleTimeoutMS(timeoutMS int64) string {
	for _, choice := range HTTPIdleTimeoutChoices {
		if choice.TimeoutMS == timeoutMS {
			return choice.Label
		}
	}
	return fmt.Sprintf("%d sec", timeoutMS/1000)
}

// ApplyHTTPProxySettings seeds the proxy environment from a configured value.
// D240: upstream resolves a proxy per target through ai/utils/node-http-proxy.ts
// (including a provider-scoped env and its own no_proxy parser). The port leaves
// resolution to Go's http.ProxyFromEnvironment on the shared transport, so only
// the process environment participates and no_proxy follows httpproxy.
func ApplyHTTPProxySettings(httpProxy string) {
	proxy := strings.TrimSpace(httpProxy)
	if proxy == "" {
		return
	}
	if os.Getenv("HTTP_PROXY") == "" {
		_ = os.Setenv("HTTP_PROXY", proxy)
	}
	if os.Getenv("HTTPS_PROXY") == "" {
		_ = os.Setenv("HTTPS_PROXY", proxy)
	}
}
