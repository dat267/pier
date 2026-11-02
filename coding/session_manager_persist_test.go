package coding

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// sessionFileRoles returns the `type` of each line in a session file.
func sessionFileRoles(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			Type    string `json:"type"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatal(err)
		}
		role := probe.Type
		if probe.Type == "message" {
			role = probe.Message.Role
		}
		roles = append(roles, role)
	}
	return roles
}

// TestSessionFileCreationFollowsTheFirstMessage ports the "session file
// creation" cases of session-manager/file-operations.test.ts (ff72faba2).
func TestSessionFileCreationFollowsTheFirstMessage(t *testing.T) {
	t.Run("only setup entries stay in memory", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendModelChange("anthropic", "claude-sonnet-4-5")
		m.AppendThinkingLevelChange("off")
		if _, err := os.Stat(m.GetSessionFile()); err == nil {
			t.Fatal("file must not exist for setup entries alone")
		}
	})
	t.Run("the first user message creates the file", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendModelChange("anthropic", "claude-sonnet-4-5")
		m.AppendMessage(createUserMessage("first question"))
		if got := strings.Join(sessionFileRoles(t, m.GetSessionFile()), ","); got != "session,model_change,user" {
			t.Fatalf("roles = %q", got)
		}
		reloaded, err := OpenSession(m.GetSessionFile(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		if n := len(reloaded.Projection().Messages); n != 1 {
			t.Fatalf("reloaded messages = %d", n)
		}
	})
	t.Run("later entries append without rewriting", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendMessage(createUserMessage("first question"))
		m.AppendCustomEntry("preset-state", json.RawMessage(`{"name":"plan"}`))
		m.AppendMessage(createAssistantMessageT("first answer"))
		if got := strings.Join(sessionFileRoles(t, m.GetSessionFile()), ","); got != "session,user,custom,assistant" {
			t.Fatalf("roles = %q", got)
		}
	})
}

func assistantTestMessage() *ai.AssistantMessage {
	return &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "p", Model: "m", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "hello"}},
	}
}

// TestSessionManagerPersistStopsAtAssistant pins the flush-on-first-conversation
// check to upstream's `fileEntries.some(...)`: once a user or assistant message
// is known to exist the scan must stop, otherwise every append on a long session
// re-unmarshals every entry (2.6 ms/append at 5k entries, 8 ms at 15.5k).
func TestSessionManagerPersistStopsAtAssistant(t *testing.T) {
	build := func(filler int) *SessionManager {
		m, _ := newTestSession(t)
		m.AppendMessage(createUserMessage("one"))
		m.AppendMessage(assistantTestMessage())
		for range filler {
			m.AppendMessage(createUserMessage("filler"))
		}
		return m
	}

	small, large := build(0), build(2000)
	smallAllocs := testing.AllocsPerRun(10, func() { small.AppendMessage(createUserMessage("x")) })
	largeAllocs := testing.AllocsPerRun(10, func() { large.AppendMessage(createUserMessage("x")) })

	// Appending must not get more expensive as the session grows.
	if largeAllocs > smallAllocs+200 {
		t.Fatalf("append allocated %.0f times on a 2k-entry session vs %.0f on a fresh one; "+
			"the assistant scan did not short-circuit", largeAllocs, smallAllocs)
	}
}

// An existing 0-byte session file is initialized like upstream's
// _setSessionFile empty-file branch: the header is written, later turns append,
// and no write error is recorded (M74).
func TestOpeningEmptySessionFileInitializesIt(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.jsonl"
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	if failures := manager.DrainWriteErrors(); len(failures) != 0 {
		t.Fatalf("write errors: %v", failures)
	}

	lines := strings.Split(strings.TrimSpace(readFileString(t, path)), "\n")
	if len(lines) != 2 {
		t.Fatalf("session file has %d lines, want header plus one message:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	header, err := UnmarshalFileEntry(lines[0])
	if err != nil || header.Header == nil || header.Header.ID != manager.GetSessionID() {
		t.Fatalf("first line is not the session header: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"role":"user"`) || !strings.Contains(lines[1], "hi") {
		t.Fatalf("second line is not the user message: %s", lines[1])
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("session file mode = %v, want 0600", info.Mode().Perm())
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
