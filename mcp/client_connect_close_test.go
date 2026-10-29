package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

type initializedSendBarrier struct {
	Transport
	entered chan struct{}
	release chan struct{}
}

func (b *initializedSendBarrier) Send(ctx context.Context, message Message) error {
	err := b.Transport.Send(ctx, message)
	if notification, ok := message.(*protocol.JsonRpcNotification); ok && notification.Method == "notifications/initialized" && err == nil {
		close(b.entered)
		<-b.release // Dispatch already succeeded; hold only its completion.
	}
	return err
}

// D302: pi v1.4.0 (4a080252), packages/mcp/src/client.ts connect() also
// assigns connected unconditionally after await notifyInternal. Pier preserves
// the terminal closed state if Close or a transport drop won in that interval.
func TestClientConnectDoesNotResurrectClosedConnection(t *testing.T) {
	for _, mode := range []string{"control", "client close", "peer drop"} {
		t.Run(mode, func(t *testing.T) {
			server, transport := newTestServer()
			server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
				return map[string]any{
					"protocolVersion": protocol.LatestProtocolVersion,
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "fixture", "version": "1"},
				}, nil
			})
			barrier := &initializedSendBarrier{Transport: transport, entered: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := NewClient(ClientOptions{Name: "fixture", Version: "1"})
			defer client.Close(context.Background())
			done := make(chan error, 1)
			go func() { _, err := client.Connect(ctx, barrier); done <- err }()
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch mode {
			case "client close":
				if err := client.Close(ctx); err != nil {
					t.Fatal(err)
				}
			case "peer drop":
				if err := server.transport.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			release()
			var err error
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode == "control" {
				if err != nil || client.ConnectionState() != StateConnected {
					t.Fatalf("control connect err=%v state=%s", err, client.ConnectionState())
				}
				return
			}
			var closed *protocol.McpConnectionClosedError
			if !errors.As(err, &closed) || client.ConnectionState() != StateClosed {
				t.Fatalf("closed client resurrected: connect err=%v state=%s", err, client.ConnectionState())
			}
		})
	}
}
