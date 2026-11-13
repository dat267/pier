package oauth

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

// The redirect listener is loopback-only, but any local process can connect and
// send nothing. Without ReadHeaderTimeout that connection holds a handler
// goroutine and a descriptor for as long as it likes (gosec G112).
func TestCallbackServerDropsAStalledConnection(t *testing.T) {
	previous := callbackReadHeaderTimeout
	callbackReadHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(func() { callbackReadHeaderTimeout = previous })

	server, err := ListenCallbackServer(context.Background(), OAuthCallbackServerOptions{Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close(context.Background()) }()
	parsed, err := url.Parse(server.RedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("the server answered a connection that sent no request")
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("the server kept a stalled connection open for %v", 2*time.Second)
		}
	}
}
