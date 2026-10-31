package coding

import (
	"runtime"
	"strings"
	"testing"
	"weak"
)

// pi v1.4.0 (4a080252), core/session-manager.ts SessionManager.newSession
// replaces fileEntries and clears byId. Port-local read caches must not keep
// the discarded session's payload alive when the manager starts a new one.
func TestNewSessionReleasesPreviousPayload(t *testing.T) {
	for _, reader := range []string{"none", "signature", "branch", "projection", "retained snapshot"} {
		t.Run(reader, func(t *testing.T) {
			manager := InMemorySession(t.TempDir(), nil, nil)
			payload, snapshot := cachePreviousSessionPayload(manager, reader)
			manager.NewSession(nil)
			runtime.GC()
			if reader == "retained snapshot" {
				if payload.Value() == nil {
					t.Fatal("new session invalidated a caller-owned branch snapshot")
				}
			} else if payload.Value() != nil {
				t.Fatal("new session retains the previous session's message payload")
			}
			runtime.KeepAlive(snapshot)
			runtime.KeepAlive(manager)
		})
	}
}

func cachePreviousSessionPayload(manager *SessionManager, reader string) (weak.Pointer[byte], []SessionEntry) {
	id := manager.AppendMessage(createUserMessage(strings.Repeat("payload", 1024)))
	payload := weak.Make(&manager.GetEntry(id).Message[0])
	switch reader {
	case "signature":
		manager.ContextSignature()
	case "branch":
		manager.GetBranch("")
	case "projection":
		manager.Projection()
	case "retained snapshot":
		return payload, manager.GetBranch("")
	}
	return payload, nil
}
