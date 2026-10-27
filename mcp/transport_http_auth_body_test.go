package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

type recoveryBody struct {
	io.Reader
	closes int
}

func (b *recoveryBody) Close() error { b.closes++; return nil }

type recoveryAuthProvider struct{ err error }

func (*recoveryAuthProvider) Token(context.Context) (string, error)                    { return "synthetic-token", nil }
func (p *recoveryAuthProvider) OnUnauthorized(context.Context, UnauthorizedInfo) error { return p.err }

// pi v1.4.0 (4a080252), transports/streamable-http.ts authorizedFetch discards
// the unauthorized response in finally, before retrying, even when recovery
// succeeds. AuthProvider does not own the transport's response-body cleanup.
func TestStreamableHTTPClosesUnauthorizedBodyBeforeRetry(t *testing.T) {
	for _, recoveryFails := range []bool{false, true} {
		name := "recovered"
		if recoveryFails {
			name = "failed-control"
		}
		t.Run(name, func(t *testing.T) {
			authErr := errors.New("auth failed")
			auth := &recoveryAuthProvider{}
			if recoveryFails {
				auth.err = authErr
			}
			body := &recoveryBody{Reader: strings.NewReader("authentication required")}
			calls := 0
			closedBeforeRetry := false
			transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
				URL: "https://invalid.test", AuthProvider: auth, OpenGetStream: new(false),
				Fetch: func(context.Context, *http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: body}, nil
					}
					closedBeforeRetry = body.closes > 0
					return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
				},
			})
			if err := transport.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer transport.Close(context.Background())
			err := transport.Send(context.Background(), &protocol.JsonRpcNotification{JSONRPC: protocol.JSONRPCVersion, Method: "notifications/fixture"})
			if recoveryFails {
				if !errors.Is(err, authErr) || calls != 1 {
					t.Fatalf("failed recovery = %v, calls = %d", err, calls)
				}
			} else {
				if err != nil || calls != 2 {
					t.Fatalf("recovered Send = %v, calls = %d", err, calls)
				}
				if !closedBeforeRetry {
					t.Error("unauthorized response body remained open when retry dispatched")
				}
			}
			if body.closes != 1 {
				t.Fatalf("unauthorized body closes = %d, want 1", body.closes)
			}
		})
	}
}
