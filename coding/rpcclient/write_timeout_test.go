package rpcclient

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRPCClientUnreadStdinPeer(t *testing.T) {
	if os.Getenv("PIER_RPC_UNREAD_STDIN_PEER") != "1" {
		return
	}
	wait := make(chan os.Signal, 1)
	signal.Notify(wait, os.Interrupt, syscall.SIGTERM)
	fmt.Println(`{"type":"peer_ready"}`)
	<-wait
	os.Exit(0)
}

// Reference: pi v1.4.0 (4a080252), modes/rpc/rpc-client.ts send arms its
// response timeout before stdin.write, whose backpressure never blocks JS.
func TestClientRequestTimeoutIncludesBlockedStdinWrite(t *testing.T) {
	client := New(Options{CLIPath: os.Args[0], PeerArgs: []string{"-test.run=^TestRPCClientUnreadStdinPeer$"}, Env: map[string]string{"PIER_RPC_UNREAD_STDIN_PEER": "1"}, RequestTimeout: 100 * time.Millisecond})
	ready := make(chan struct{})
	unsubscribe := client.OnEvent(func(event Event) {
		if event.Type() == "peer_ready" {
			close(ready)
		}
	})
	defer unsubscribe()
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Stop()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("peer did not become ready")
	}
	done := make(chan error, 1)
	go func() { _, err := client.Prompt(strings.Repeat("x", 256*1024), nil, ""); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Timeout waiting for response to prompt") {
			t.Fatalf("blocked write error = %v, want request timeout", err)
		}
	case <-time.After(2 * time.Second):
		client.Stop()
		<-done
		t.Fatal("request timeout did not bound a blocked stdin write")
	}
}
