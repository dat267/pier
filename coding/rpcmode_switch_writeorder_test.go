package coding

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
)

// The write queue is shared with the replacement manager, so a write queued for
// the outgoing session must land before switch_session answers: otherwise it
// runs against a session that is already gone, and if the replacement wrote the
// same path the claim fails with "file exists" (D493).
func TestRPCSwitchSessionDrainsTheOutgoingWriteQueue(t *testing.T) {
	queue := offloop.New()
	t.Cleanup(queue.Stop)
	blocked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	queue.Go(func() {
		close(blocked)
		<-release
	})
	<-blocked

	cwd := t.TempDir()
	persist := true
	manager := NewSessionManager(cwd, &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist, WriteQueue: queue})
	session, err := NewAgentSession(&SessionConfig{Cwd: cwd, StreamFn: stubStreamFn, Sessions: manager})
	if err != nil {
		t.Fatal(err)
	}
	// The append queues its file write behind the blocked task.
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "outgoing"}, Timestamp: 1})
	oldPath := manager.GetSessionFile()
	if oldPath == "" {
		t.Fatal("outgoing session has no file")
	}

	target := NewSessionManager(cwd, &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
	target.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "resumed"}, Timestamp: 1})
	targetPath := target.GetSessionFile()

	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"s","type":"switch_session","sessionPath":`+strconv.Quote(targetPath)+`}`)
	select {
	case record := <-client.output:
		if record["type"] == "response" && record["id"] == "s" {
			t.Fatalf("switch_session answered with the outgoing write still queued: %+v", record)
		}
	case <-time.After(200 * time.Millisecond):
	}
	unblock()
	response := client.until(t, byResponseID("s"))
	if response["success"] != true {
		t.Fatalf("switch_session response = %+v", response)
	}
	data := readFileBytes(t, oldPath)
	if !strings.Contains(string(data), `"outgoing"`) {
		t.Fatalf("the outgoing write never reached its file: %s", data)
	}
}
