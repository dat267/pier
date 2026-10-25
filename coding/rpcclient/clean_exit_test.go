package rpcclient

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRPCClientCleanExitPeer(t *testing.T) {
	if os.Getenv("PIER_RPC_CLEAN_EXIT_PEER") != "1" {
		t.Skip("subprocess fixture")
	}
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	os.Exit(0)
}

// pi v1.4.0 modes/rpc/rpc-client.ts createProcessExitError receives code=0
// for a normal exit, not null (null represents signal termination).
func TestClientReportsZeroExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := New(Options{
		CLIPath: os.Args[0], PeerArgs: []string{"-test.run=^TestRPCClientCleanExitPeer$"},
		Env: map[string]string{"PIER_RPC_CLEAN_EXIT_PEER": "1"}, RequestTimeout: 3 * time.Second,
	})
	t.Cleanup(client.Stop)
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := client.State()
	if err == nil || !strings.Contains(err.Error(), "Agent process exited (code=0 signal=null)") {
		t.Fatalf("normal exit reported as %v, want code=0 signal=null", err)
	}
}
