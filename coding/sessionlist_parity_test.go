package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type sessionListEntry struct {
	ID                string  `json:"id"`
	Cwd               string  `json:"cwd"`
	Name              *string `json:"name"`
	ParentSessionPath *string `json:"parentSessionPath"`
	Created           string  `json:"created"`
	Modified          string  `json:"modified"`
	MessageCount      int     `json:"messageCount"`
	FirstMessage      string  `json:"firstMessage"`
	AllMessagesText   string  `json:"allMessagesText"`
}

// TestSessionListMatchesUpstream pins the listing metadata for session files,
// including `modified`: only user/assistant messages that carry a content
// member count, using the message timestamp or the entry timestamp.
// Regenerate with
// node coding/testdata/session-list/generate.mjs ./pi > coding/testdata/session-list/golden.json
func TestSessionListMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/session-list/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Lines    []string           `json:"lines"`
		Sessions []sessionListEntry `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture) == 0 {
		t.Fatal("fixture is empty")
	}
	for name, test := range fixture {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			content := strings.Join(test.Lines, "\n") + "\n"
			if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			sessions := ListSessions("/work", dir)
			got := make([]sessionListEntry, 0, len(sessions))
			for _, session := range sessions {
				entry := sessionListEntry{
					ID:              session.ID,
					Cwd:             session.Cwd,
					Name:            nil,
					Created:         session.Created.UTC().Format("2006-01-02T15:04:05.000Z"),
					Modified:        session.Modified.UTC().Format("2006-01-02T15:04:05.000Z"),
					MessageCount:    session.MessageCount,
					FirstMessage:    session.FirstMessage,
					AllMessagesText: session.AllMessagesText,
				}
				if session.Name != "" {
					value := session.Name
					entry.Name = &value
				}
				if session.ParentSessionPath != "" {
					value := session.ParentSessionPath
					entry.ParentSessionPath = &value
				}
				got = append(got, entry)
			}
			if !reflect.DeepEqual(got, test.Sessions) {
				t.Errorf("sessions = %+v, want %+v", got, test.Sessions)
			}
		})
	}
}
