package coding

import (
	"runtime"
	"testing"
	"weak"
)

// pi v1.4.0 (4a080252), core/agent-session.ts subscribe removes listeners
// with Array.splice. A detached UI's captured state must not survive solely in
// the Go listener slice's unused backing tail.
func TestSessionUnsubscribeReleasesListenerCapture(t *testing.T) {
	dir := t.TempDir()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, AgentDir: dir, Sessions: InMemorySession(dir, nil, nil), StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Dispose)
	payload := subscribeThenRemoveSessionCapture(session)
	runtime.GC()
	if payload.Value() != nil {
		t.Fatal("unsubscribed session listener retains captured payload")
	}
	runtime.KeepAlive(session)
}

func subscribeThenRemoveSessionCapture(session *AgentSession) weak.Pointer[[1024]byte] {
	payload := new([1024]byte)
	pointer := weak.Make(payload)
	remove := session.Subscribe(func(*SessionEvent) { runtime.KeepAlive(payload) })
	remove()
	return pointer
}
