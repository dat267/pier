package coding

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 (f9a26365), modes/rpc/rpc-mode.ts awaits
// session.abort. D235 also includes admitted auth-preflight workers in the Go
// abort contract, without waiting for independently running user bash.
func TestRPCAbortWaitsForCanceledPreflightToSettle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := stubProvider("gate")
		var enabled atomic.Bool
		started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var startedOnce, canceledOnce, releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		provider.Auth.APIKey.Resolve = func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
			if enabled.Load() {
				startedOnce.Do(func() { close(started) })
				<-input.Ctx2.Done()
				canceledOnce.Do(func() { close(canceled) })
				<-release // Cancellation was observed, but cleanup is not done.
				return nil, input.Ctx2.Err()
			}
			return nil, nil
		}
		runtime := runtimeWithProviders(t, provider)
		session := rpcTestSession(t)
		session.control.ModelRuntime = runtime
		session.Agent.SetModel(runtime.GetModel("gate", "gate-model"))
		enabled.Store(true)
		client := startRPCTest(t, session)
		client.send(t, `{"id":"prompt","type":"prompt","message":"hello"}`)
		<-started
		client.send(t, `{"id":"abort","type":"abort"}`)
		<-canceled
		synctest.Wait()
		select {
		case record := <-client.output:
			if record["id"] == "abort" {
				t.Errorf("abort acknowledged before canceled prompt preflight settled: %+v", record)
			}
			client.output <- record
		default:
		}
		unblock()
		responses := client.untilResponses(t, "prompt", "abort")
		if responses["abort"]["success"] != true || responses["prompt"]["success"] != false {
			t.Fatalf("responses after preflight settlement = %+v", responses)
		}
	})
}
