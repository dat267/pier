package mcp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Closing a client while calls are in flight, from several goroutines at once,
// must settle every call, return from every Close, and notify close listeners
// exactly once. Upstream client.ts (pi v1.5.0 f9a26365) rejects pending
// requests on close; the Go port adds the concurrency the event loop excludes.
func TestClientCloseRacingInFlightCallsSettlesEverything(t *testing.T) {
	const rounds, callers, closers = 30, 8, 3
	for round := range rounds {
		client, server := connectTestClient(t)
		var closes atomic.Int32
		client.OnClose(func() { closes.Add(1) })
		release := blockUntilCleanup(t)
		server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
			if round%2 == 0 {
				return map[string]any{"content": []any{}}, nil
			}
			<-release
			return map[string]any{"content": []any{}}, nil
		})

		var wg sync.WaitGroup
		for range callers {
			wg.Go(func() {
				_, _ = client.CallTool(context.Background(), "tool", map[string]any{}, RequestOptions{})
			})
		}
		for range closers {
			wg.Go(func() { _ = client.Close(context.Background()) })
		}
		settled := make(chan struct{})
		go func() { wg.Wait(); close(settled) }()
		select {
		case <-settled:
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: a call or Close did not return", round)
		}
		if got := closes.Load(); got != 1 {
			t.Fatalf("round %d: close listeners ran %d times, want 1", round, got)
		}
		if state := client.ConnectionState(); state != StateClosed {
			t.Fatalf("round %d: state = %s", round, state)
		}
	}
}

// One request settled by its response, its timeout and its caller's
// cancellation at about the same moment returns exactly once, and nothing is
// left pending. Upstream client.ts (pi v1.5.0 f9a26365) settles each request
// once on its event loop; here the three settle from different goroutines.
func TestClientRequestSettledFromThreeSidesAtOnceReturnsOnce(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		time.Sleep(time.Millisecond)
		return map[string]any{"content": []any{}}, nil
	})
	const calls = 200
	var wg sync.WaitGroup
	var returned atomic.Int32
	for index := range calls {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(index%3)*time.Millisecond)
			defer cancel()
			// The timeout lands near the 1ms response; so does the cancel.
			_, _ = client.CallTool(ctx, "tool", map[string]any{}, RequestOptions{TimeoutMs: 1 + int64(index%2), TimeoutMsSet: true})
			returned.Add(1)
		})
	}
	settled := make(chan struct{})
	go func() { wg.Wait(); close(settled) }()
	select {
	case <-settled:
	case <-time.After(10 * time.Second):
		t.Fatalf("%d of %d calls returned", returned.Load(), calls)
	}
	client.mu.Lock()
	pending, progress := len(client.pending), len(client.progressRequests)
	client.mu.Unlock()
	if pending != 0 || progress != 0 {
		t.Fatalf("left pending=%d progress=%d after every call returned", pending, progress)
	}
}
