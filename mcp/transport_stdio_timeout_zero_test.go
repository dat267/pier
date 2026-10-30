package mcp

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// pi v1.4.0 stdio.ts uses ?? for closeTimeoutMs, so zero is explicit and its
// setTimeout delays clamp to 1ms rather than selecting the 2s default.
func TestStdioTransportExplicitZeroCloseTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stubborn fixture relies on POSIX signals")
	}
	transport := NewStdioTransport(StdioTransportOptions{Command: stubbornFixtureBin, CloseTimeoutMs: 0, CloseTimeoutMsSet: true})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := transport.PID()
	var grandchild int
	deadline := time.Now().Add(5 * time.Second)
	for grandchild == 0 {
		fmt.Sscanf(transport.Stderr(), "grandchild %d", &grandchild)
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid never appeared; stderr = %q", transport.Stderr())
		}
		time.Sleep(10 * time.Millisecond)
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
		t.Fatal("explicit zero close timeout selected the default delay")
	}
	for range 100 {
		alive, err := processAlive(grandchild)
		if err != nil {
			t.Fatalf("probe grandchild: %v", err)
		}
		if !alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived zero-timeout close", grandchild)
}
