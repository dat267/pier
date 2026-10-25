package rpcclient

import (
	"context"
	"os"
	"sync"
	"testing"
)

// pi v1.4.0 modes/rpc/rpc-client.ts start claims this.process before its first
// await, so overlapping starts cannot spawn more than one peer. Go must make
// that admission atomic before releasing its mutex to spawn the subprocess.
func TestClientConcurrentStartAdmitsOnePeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := New(Options{
		CLIPath: os.Args[0], PeerArgs: []string{"-test.run=^TestRPCClientFakeServer$"},
		Env: map[string]string{"PIER_RPC_CLIENT_FAKE": "1"},
	})
	t.Cleanup(func() { cancel(); client.Stop() })
	const callers = 8
	gate := make(chan struct{})
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-gate
			results <- client.Start(ctx)
		}()
	}
	ready.Wait()
	close(gate)
	admitted := 0
	for range callers {
		err := <-results
		if err == nil {
			admitted++
		} else if err.Error() != "Client already started" {
			t.Errorf("unexpected startup error: %v", err)
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted %d overlapping starts, want exactly 1", admitted)
	}
	if _, err := client.State(); err != nil {
		t.Fatalf("admitted peer is not usable: %v", err)
	}
}
