package ai

import (
	"fmt"
	"net"
	"testing"
)

// holdPort occupies a fixed OAuth callback port for the test, so the
// callback server takes its listen-error path. A port that is already in use
// serves the same purpose.
func holdPort(t *testing.T, host string, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return // already busy
	}
	t.Cleanup(func() { _ = listener.Close() })
}

// With the fixed port taken, upstream's codex login (openai-codex.ts, v1.5.0
// f9a26365) reports a null wait and falls back to manual entry. Handing over
// to manual entry and closing must not settle the server a second time.
func TestCodexCallbackServerWithItsPortTakenSettlesOnce(t *testing.T) {
	holdPort(t, oauthCallbackHost(), OpenAICodexCallbackPort)
	server := StartOpenAICodexCallbackServer("state")
	if !server.unavailable {
		t.Skip("the callback port could not be made unavailable")
	}
	if code, ok := server.WaitForCode(); ok || code != "" {
		t.Fatalf("WaitForCode = %q, %v; want no code", code, ok)
	}
	server.CancelWait()
	server.Close()
}

// Radius's browser login (radius.ts, v1.5.0 f9a26365) errors when its fixed
// port is taken; its deferred Close must not panic on the way out.
func TestRadiusCallbackServerWithItsPortTakenSettlesOnce(t *testing.T) {
	holdPort(t, radiusCallbackHost, RadiusCallbackPort)
	server := StartRadiusCallbackServer("state", nil)
	if !server.unavailable {
		t.Skip("the callback port could not be made unavailable")
	}
	if code, ok := server.WaitForCode(); ok || code != "" {
		t.Fatalf("WaitForCode = %q, %v; want no code", code, ok)
	}
	server.Close()
}
