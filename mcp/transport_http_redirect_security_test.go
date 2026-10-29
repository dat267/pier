package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

func TestStreamableHTTPRedirectLimitRemainsTen(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, r.URL.String(), http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: server.URL})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := transport.Send(context.Background(), &protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("redirect limit error = %v", err)
	}
	if got := requests.Load(); got != 10 {
		t.Fatalf("redirect requests = %d, want 10", got)
	}
}

// pi v1.4.0 (4a080252), packages/mcp/src/transports/streamable-http.ts uses
// Fetch, which removes sensitive authentication headers on an origin change.
func TestStreamableHTTPRedirectDoesNotForwardAuthorizationAcrossPorts(t *testing.T) {
	for _, crossOrigin := range []bool{false, true} {
		name := "same-origin"
		if crossOrigin {
			name = "different-port"
		}
		t.Run(name, func(t *testing.T) {
			received := make(chan http.Header, 1)
			finish := func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			}
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { finish(w, r) }))
			defer target.Close()
			var origin *httptest.Server
			origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					destination := "/finish"
					if crossOrigin {
						destination = target.URL
					}
					http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
					return
				}
				finish(w, r)
			}))
			defer origin.Close()
			transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
				URL: origin.URL + "/start",
				Headers: map[string]string{
					"Authorization":       "Bearer synthetic-secret",
					"Proxy-Authorization": "Basic synthetic-secret",
					"Cookie":              "synthetic-cookie=value",
					"Cookie2":             "synthetic-cookie2=value",
				},
			})
			if err := transport.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = transport.Close(context.Background()) })
			err := transport.Send(context.Background(), &protocol.JsonRpcRequest{
				JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			got := <-received
			for name, want := range map[string]string{
				"Authorization":       "Bearer synthetic-secret",
				"Proxy-Authorization": "Basic synthetic-secret",
				"Cookie":              "synthetic-cookie=value",
				"Cookie2":             "synthetic-cookie2=value",
			} {
				if crossOrigin {
					want = ""
				}
				if value := got.Get(name); value != want {
					t.Errorf("redirect destination %s = %q, want %q", name, value, want)
				}
			}
		})
	}
}
