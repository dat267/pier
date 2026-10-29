package mcp

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

type stalledAcknowledgmentBody struct {
	ctx    context.Context
	closed bool
}

func (b *stalledAcknowledgmentBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledAcknowledgmentBody) Close() error {
	b.closed = true
	return nil
}

// pi v1.4.0 (4a080252), transports/streamable-http.ts send/discard:
// notification acknowledgments ignore and cancel any response body.
func TestStreamableHTTPDoesNotDrainNotificationAcknowledgment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var body *stalledAcknowledgmentBody
		openGetStream := false
		transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
			URL: "https://fixture.invalid/mcp", OpenGetStream: &openGetStream,
			Fetch: func(ctx context.Context, _ *http.Request) (*http.Response, error) {
				body = &stalledAcknowledgmentBody{ctx: ctx}
				return &http.Response{StatusCode: http.StatusAccepted, Body: body}, nil
			},
		})
		if err := transport.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer transport.Close(context.Background())
		if err := transport.Send(ctx, &protocol.JsonRpcNotification{JSONRPC: "2.0", Method: "probe"}); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatalf("ignored 202 body exhausted sender budget: %v", ctx.Err())
		}
		if body == nil || !body.closed {
			t.Fatal("acknowledgment body not closed")
		}
	})
}
