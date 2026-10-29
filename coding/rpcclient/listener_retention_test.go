package rpcclient_test

import (
	"runtime"
	"testing"
	"weak"

	"github.com/dat267/pier/coding/rpcclient"
)

// pi v1.4.0 (4a080252), modes/rpc/rpc-client.ts onEvent removes listeners with
// Array.splice. CollectEvents/WaitForIdle unsubscribe during ordinary use, so a
// removed listener must not keep its captured event history alive.
func TestOnEventUnsubscribeReleasesListenerCapture(t *testing.T) {
	client := rpcclient.New(rpcclient.Options{})
	payload := new([1024]byte)
	pointer := weak.Make(payload)
	remove := client.OnEvent(func(rpcclient.Event) { runtime.KeepAlive(payload) })
	remove()
	runtime.GC()
	if pointer.Value() != nil {
		t.Fatal("unsubscribed RPC event listener retains captured payload")
	}
	runtime.KeepAlive(client)
}
