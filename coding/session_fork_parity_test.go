package coding

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.3.0 core/session-manager.ts forkFrom appends one newline
// per JSON.stringify(entry). Extra blank lines violate the JSONL byte contract.
func TestForkSessionWritesExactlyOneNewlinePerEntry(t *testing.T) {
	tempAgentDir(t)
	source := NewSessionManager(t.TempDir(), nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
	source.AppendMessage(&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop})
	for _, branch := range []bool{false, true} {
		name := "whole"
		if branch {
			name = "branch"
		}
		t.Run(name, func(t *testing.T) {
			var fork *SessionManager
			var err error
			if branch {
				fork, err = ForkSessionAtEntry(source, *source.GetLeafID(), source.GetCwd(), t.TempDir(), nil)
			} else {
				fork, err = ForkSession(source.GetSessionFile(), source.GetCwd(), t.TempDir(), nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(fork.GetSessionFile())
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.Count(string(data), "\n"), len(fork.GetEntries())+1; got != want {
				t.Fatalf("JSONL newlines=%d want=%d", got, want)
			}
		})
	}
}

// Reference: session-manager.ts propagates JSON.stringify failures instead of
// silently omitting an entry from the fork's transcript.
func TestForkSessionRejectsUnserializableEntries(t *testing.T) {
	persist := false
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
	id := source.AppendCustomEntry("broken", json.RawMessage(`{`))
	if fork, err := ForkSessionAtEntry(source, id, source.GetCwd(), t.TempDir(), nil); err == nil {
		t.Fatalf("invalid entry was silently dropped; fork=%v", fork.GetEntries())
	}
}
