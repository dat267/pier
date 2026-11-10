package rpcclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestRPCClientEchoPeer answers every command with an empty success.
func TestRPCClientEchoPeer(t *testing.T) {
	if os.Getenv("PIER_RPC_ECHO_PEER") != "1" {
		t.Skip("subprocess fixture")
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command map[string]any
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			continue
		}
		reply, _ := json.Marshal(map[string]any{
			"type": "response", "id": command["id"], "command": command["type"], "success": true, "data": map[string]any{},
		})
		fmt.Println(string(reply))
	}
	os.Exit(0)
}

// Stopping the client while requests are in flight, from several goroutines,
// must settle every request and every Stop, and later requests must fail
// rather than hang. Upstream rpc-client.ts (pi v1.5.0 f9a26365) rejects
// pending requests when the process stops; here callers overlap.
func TestRPCClientStopRacingRequestsSettles(t *testing.T) {
	const rounds = 8
	for round := range rounds {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		client := New(Options{
			CLIPath: os.Args[0], PeerArgs: []string{"-test.run=^TestRPCClientEchoPeer$"},
			Env: map[string]string{"PIER_RPC_ECHO_PEER": "1"}, RequestTimeout: 3 * time.Second,
		})
		if err := client.Start(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				for range 20 {
					_, _ = client.send(map[string]any{"type": "get_state"})
				}
			})
		}
		wg.Go(func() {
			time.Sleep(time.Duration(round) * 300 * time.Microsecond)
			client.Stop()
		})
		wg.Go(client.Stop)
		settled := make(chan struct{})
		go func() { wg.Wait(); close(settled) }()
		select {
		case <-settled:
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatalf("round %d: a request or Stop did not return", round)
		}
		if _, err := client.send(map[string]any{"type": "get_state"}); err == nil {
			t.Fatalf("round %d: a request after Stop succeeded", round)
		}
		client.Stop()
		cancel()
	}
}
