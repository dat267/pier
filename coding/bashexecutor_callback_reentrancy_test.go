package coding

import (
	"context"
	"testing"
	"time"
)

type reentrantBashOperations struct {
	onData     func([]byte)
	nestedDone <-chan struct{}
}

func (operations *reentrantBashOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	operations.onData = options.OnData
	options.OnData([]byte("outer"))
	if operations.nestedDone != nil {
		<-operations.nestedDone
	}
	code := 0
	return &code, nil
}

// pi v1.4.0 calls onChunk synchronously from appendText without a mutex.
// Likewise, arbitrary output callbacks must not run under the Go state lock.
func TestBashOutputCallbackMayReenterOutput(t *testing.T) {
	operations := &reentrantBashOperations{}
	nestedDone := make(chan struct{})
	operations.nestedDone = nestedDone
	nestedRanDuringCallback := false

	result, err := ExecuteBashWithOperations(context.Background(), "fixture", t.TempDir(), operations, &BashExecutorOptions{
		OnChunk: func(chunk string) {
			if chunk != "outer" {
				return
			}
			go func() {
				operations.onData([]byte("nested"))
				close(nestedDone)
			}()
			select {
			case <-nestedDone:
				nestedRanDuringCallback = true
			case <-time.After(100 * time.Millisecond):
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !nestedRanDuringCallback {
		t.Fatal("reentrant OnData could not finish while OnChunk was running")
	}
	if result.Output != "outernested" {
		t.Fatalf("output = %q, want outer nested output in order", result.Output)
	}
}
