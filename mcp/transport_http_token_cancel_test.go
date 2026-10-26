package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

type heldTokenProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *heldTokenProvider) Token(context.Context) (string, error) {
	close(p.entered)
	<-p.release
	return "synthetic-token", nil
}

func (*heldTokenProvider) OnUnauthorized(context.Context, UnauthorizedInfo) error { return nil }

// pi v1.4.0 (4a080252), transports/streamable-http.ts authorizedFetch checks
// the sender's signal after token acquisition and before calling fetch. Shared
// token acquisition is not canceled, but an abandoned call must not dispatch.
func TestStreamableHTTPCanceledTokenWaitDoesNotDispatch(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		name := "control"
		if cancelRequest {
			name = "canceled"
		}
		t.Run(name, func(t *testing.T) {
			auth := &heldTokenProvider{entered: make(chan struct{}), release: make(chan struct{})}
			calls := 0
			transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
				URL: "https://invalid.test", AuthProvider: auth, OpenGetStream: boolPtr(false),
				Fetch: func(context.Context, *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`))}, nil
				},
			})
			if err := transport.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = transport.Close(context.Background()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- transport.Send(ctx, &protocol.JsonRpcRequest{JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "tools/call"})
			}()
			select {
			case <-auth.entered:
			case <-time.After(3 * time.Second):
				close(auth.release)
				t.Fatal("token lookup did not start")
			}
			if cancelRequest {
				cancel()
			}
			close(auth.release)
			select {
			case err := <-done:
				if cancelRequest && !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled Send = %v", err)
				}
				if !cancelRequest && err != nil {
					t.Fatalf("control Send = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Send did not settle after token release")
			}
			wantCalls := 1
			if cancelRequest {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("fetch dispatches = %d, want %d", calls, wantCalls)
			}
		})
	}
}
