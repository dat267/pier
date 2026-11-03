package rpcclient

import (
	"bufio"
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRPCClientClosedStdinPeer(t *testing.T) {
	if os.Getenv("PIER_RPC_CLOSED_STDIN_PEER") != "1" {
		t.Skip("subprocess fixture")
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	if _, err := bufio.NewReader(os.Stdin).ReadBytes('\n'); err != nil {
		os.Exit(2)
	}
	_ = os.Stdin.Close()
	_, _ = os.Stdout.WriteString(`{"type":"stdin_closed"}` + "\n")
	<-signals
	os.Exit(0)
}

// Reference: pi v1.5.0 (f9a26365), modes/rpc/rpc-client.ts
// childProcess.stdin's error handler rejects every pending request, not just
// the command whose write detected that the peer stopped reading.
func TestClientStdinFailureRejectsEarlierPendingRequests(t *testing.T) {
	client := New(Options{
		CLIPath:        os.Args[0],
		PeerArgs:       []string{"-test.run=^TestRPCClientClosedStdinPeer$"},
		Cwd:            t.TempDir(),
		Env:            map[string]string{"PIER_RPC_CLOSED_STDIN_PEER": "1", "PI_CODING_AGENT_DIR": t.TempDir()},
		RequestTimeout: 10 * time.Second,
	})
	t.Cleanup(client.Stop)
	stdinClosed := make(chan struct{})
	unsubscribe := client.OnEvent(func(event Event) {
		if event.Type() == "stdin_closed" {
			close(stdinClosed)
		}
	})
	defer unsubscribe()
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { _, err := client.State(); first <- err }()
	select {
	case <-stdinClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("peer did not accept the first request and close its stdin")
	}
	_, secondErr := client.Messages()
	if secondErr == nil || !strings.Contains(secondErr.Error(), "Agent process stdin error:") {
		t.Fatalf("second request error = %v, want stdin failure", secondErr)
	}
	select {
	case err := <-first:
		if err == nil || !strings.Contains(err.Error(), "Agent process stdin error:") {
			t.Fatalf("earlier pending request error = %v, want stdin failure", err)
		}
	case <-time.After(time.Second):
		client.Stop()
		<-first
		t.Fatal("stdin failure left an earlier request pending until its unrelated timeout or process exit")
	}
}
