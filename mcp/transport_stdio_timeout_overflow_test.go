package mcp

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// pi v1.4.0 stdio.ts schedules SIGKILL with setTimeout(grace+closeTimeoutMs),
// whose runtime clamps a delay above 2^31-1 to 1ms.
func TestStdioTransportCloseTimeoutOverflowKillsPromptly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stubborn fixture relies on POSIX signals")
	}
	transport := NewStdioTransport(StdioTransportOptions{
		Command: stubbornFixtureBin, CloseTimeoutMs: 1 << 31,
	})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := transport.PID()
	if pid <= 0 {
		t.Fatalf("server pid = %d after start", pid)
	}
	var grandchild int
	deadline := time.Now().Add(5 * time.Second)
	for grandchild == 0 {
		fmt.Sscanf(transport.Stderr(), "grandchild %d", &grandchild)
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid never appeared; stderr = %q", transport.Stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if alive, err := processAlive(pid); err != nil || !alive {
		t.Fatalf("stubborn server is not alive before close: alive=%v err=%v", alive, err)
	}
	select {
	case <-transport.exitCh:
		t.Fatal("server exit channel closed before close")
	default:
	}
	closed := make(chan error, 1)
	go func() { closed <- transport.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		killProcessTree(pid, syscall.SIGKILL)
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Fatal("transport close remained blocked after cleanup kill")
		}
		t.Fatal("SIGKILL timer overflow left the stubborn server running")
	}
	for range 100 {
		alive, err := processAlive(grandchild)
		if err != nil {
			t.Fatalf("probe grandchild: %v", err)
		}
		if !alive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived close", grandchild)
}
