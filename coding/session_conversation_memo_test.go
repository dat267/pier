package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// A484: the memo must agree with a fresh scan in every order, including when
// the first conversation-starting message arrives late, and after a load.
func TestConversationMemoMatchesAFreshScan(t *testing.T) {
	bash := CreateBashExecutionMessage("echo", "x", nil, false, false, "", false, 0, nil)
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "yo"}}}

	cases := []struct {
		name  string
		input []ai.Message
		want  bool
	}{
		{"only bash", []ai.Message{bash, bash}, false},
		{"bash then user", []ai.Message{bash, user}, true},
		{"user first", []ai.Message{user, bash}, true},
		{"bash then assistant", []ai.Message{bash, assistant}, true},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		manager, _ := newTestSession(t)
		for _, m := range tc.input {
			manager.AppendMessage(m)
		}
		if got := manager.hasConversation(); got != tc.want {
			t.Fatalf("%s: hasConversation after appends = %v, want %v", tc.name, got, tc.want)
		}
		// A second call must agree with the first.
		if got := manager.hasConversation(); got != tc.want {
			t.Fatalf("%s: hasConversation called twice = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A loaded session must answer from its own entries, not a stale memo.
func TestConversationMemoRefreshesOnLoad(t *testing.T) {
	manager, dir := newTestSession(t)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	path := manager.GetSessionFile()
	if err := manager.FlushWrites(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	reopened, err := OpenSession(path, dir, dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !reopened.hasConversation() {
		t.Fatal("a loaded session with a user message reported no conversation")
	}
}
