package rpcclient

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// This peer delays the final event until a second request, which the test sends
// only after the first event listener is blocked. The reader cannot have the
// final bytes buffered when that listener starts.
func TestRPCClientExitTailPeer(t *testing.T) {
	if os.Getenv("PIER_RPC_EXIT_TAIL_PEER") != "1" {
		t.Skip("subprocess fixture")
	}
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadBytes('\n'); err != nil {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, `{"type":"first"}`)
	if _, err := reader.ReadBytes('\n'); err != nil {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, `{"type":"final"}`)
	os.Exit(43)
}

// pi v1.4.0 modes/rpc/rpc-client.ts keeps stdout subscribed through natural
// process exit. Go must not close unread output just because Cmd.Wait returns.
func TestClientNaturalExitDrainsFinalEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := New(Options{
		CLIPath: os.Args[0], PeerArgs: []string{"-test.run=^TestRPCClientExitTailPeer$"},
		Env: map[string]string{"PIER_RPC_EXIT_TAIL_PEER": "1"}, RequestTimeout: 3 * time.Second,
	})
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(func() { unblock(); client.Stop() })
	final := make(chan struct{}, 1)
	client.OnEvent(func(event Event) {
		switch event.Type() {
		case "first":
			close(entered)
			<-release
		case "final":
			final <- struct{}{}
		}
	})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	firstRequest := make(chan error, 1)
	go func() { _, err := client.State(); firstRequest <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first event did not arrive:", ctx.Err())
	}
	_, err := client.State()
	if err == nil || !strings.Contains(err.Error(), "Agent process exited (code=43") {
		t.Fatalf("second request must observe peer exit before unblocking listener: %v", err)
	}
	select {
	case err := <-firstRequest:
		if err == nil || !strings.Contains(err.Error(), "Agent process exited (code=43") {
			t.Fatalf("first request after peer exit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("first request did not settle:", ctx.Err())
	}
	unblock()
	select {
	case <-final:
	case <-time.After(3 * time.Second):
		t.Fatal("natural process exit discarded the unread final event")
	}
}
