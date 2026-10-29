package rpcclient

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// pi v1.4.0 modes/rpc/rpc-client.ts stores the spawned process before its first
// await. Stop must likewise settle a Start that has not published its process.
func TestClientStopDuringStartCancelsUnpublishedProcess(t *testing.T) {
	client := New(Options{
		CLIPath:  os.Args[0],
		PeerArgs: []string{"-test.run=^TestRPCClientFakeServer$"},
		Env:      map[string]string{"PIER_RPC_CLIENT_FAKE": "1"},
	})
	t.Cleanup(client.Stop)
	entered, release := make(chan struct{}), make(chan struct{})
	client.startCommand = func(ctx context.Context, command *exec.Cmd) error {
		if err := command.Start(); err != nil {
			return err
		}
		close(entered)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return nil
	}
	startResult := make(chan error, 1)
	startReturned := make(chan struct{})
	go func() {
		startResult <- client.Start(context.Background())
		close(startReturned)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not reach the unpublished-process window")
	}
	stopReturned := make(chan struct{}, 2)
	for range 2 {
		go func() {
			client.Stop()
			stopReturned <- struct{}{}
		}()
	}
	for range 2 {
		select {
		case <-stopReturned:
		case <-time.After(3 * time.Second):
			t.Fatal("Stop did not settle an in-progress Start")
		}
	}
	close(release)
	select {
	case <-startReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
	if err := <-startResult; err == nil {
		t.Error("Start succeeded after Stop canceled the unpublished child")
	}
	if _, err := client.State(); err == nil {
		t.Error("child remained usable after Stop canceled startup")
	}
	client.startCommand = nil
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("restart after canceled startup: %v", err)
	}
	if _, err := client.State(); err != nil {
		t.Fatalf("restarted child is not usable: %v", err)
	}
	client.Stop()
}
