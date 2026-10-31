package coding

import (
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/session-manager.ts createBranchedSession
// replaces fileEntries and rebuilds byId from only the retained path. The
// shallow entry copies preserve message identity on that path, but discarded
// messages must not remain owned by the manager's port-local read caches.
func TestCreateBranchedSessionReleasesDiscardedMessages(t *testing.T) {
	for _, reader := range []string{"memo", "signature", "branch", "projection"} {
		t.Run(reader, func(t *testing.T) {
			manager := InMemorySession(t.TempDir(), nil, nil)
			keptID := manager.AppendMessage(createUserMessage("keep"))
			kept := manager.EntryMessages(manager.GetEntry(keptID))[0]
			discardedID, raw, decoded := cacheDiscardedBranchMessage(manager, reader)
			if _, err := manager.CreateBranchedSession(keptID); err != nil {
				t.Fatal(err)
			}
			if manager.GetEntry(discardedID) != nil {
				t.Fatal("fork still contains the discarded entry")
			}
			runtime.GC()
			if raw.Value() != nil {
				t.Error("fork retains discarded raw message bytes")
			}
			if decoded.Value() != nil {
				t.Error("fork retains discarded decoded message")
			}
			if got := manager.EntryMessages(manager.GetEntry(keptID))[0]; got != kept {
				t.Error("fork changed the retained message's identity")
			}
			runtime.KeepAlive(manager)
		})
	}
}

func cacheDiscardedBranchMessage(manager *SessionManager, reader string) (string, weak.Pointer[byte], weak.Pointer[ai.UserMessage]) {
	id := manager.AppendMessage(createUserMessage(strings.Repeat("discard", 1024)))
	entry := manager.GetEntry(id)
	message := manager.EntryMessages(entry)[0].(*ai.UserMessage)
	switch reader {
	case "signature":
		manager.ContextSignature()
	case "branch":
		manager.GetBranch("")
	case "projection":
		manager.Projection()
	}
	return id, weak.Make(&entry.Message[0]), weak.Make(message)
}
