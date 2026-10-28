package coding

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/mcp"
)

// pi v1.4.0 extensions/mcp/runtime.ts stores the failure before notifying
// observers. Go observers must snapshot state and its diagnostic together.
// This small bounded race probe uses public reconnect/error-query paths;
// `go test -race` must not report a diagnostic read outside the state lock.
func TestMcpConnectionErrorsSnapshotDuringReconnect(t *testing.T) {
	var attempts atomic.Int32
	manager := NewMcpManager(context.Background(), McpManagerOptions{
		Config: LoadedMcpConfig{Servers: []McpServerEntry{
			{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
		}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return nil, fmt.Errorf("failure %d", attempts.Add(1))
		},
	})
	defer manager.Close(context.Background())
	connection := manager.Connections()[0]
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 100 {
			if _, err := connection.GetClient(context.Background()); err == nil {
				t.Error("failed transport unexpectedly connected")
			}
		}
	})
	workers.Go(func() {
		<-start
		for range 1000 {
			if got := manager.ConnectionErrors(); len(got) > 1 {
				t.Errorf("one server returned %d errors", len(got))
			}
		}
	})
	close(start)
	workers.Wait()
}
