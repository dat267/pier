package interactive

import (
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
)

// D372: renaming the active session must not append from a stale leaf. A write
// already queued on the UI's session queue can otherwise turn the newest
// message into a side branch when the file is reopened.
func TestRenamingCurrentSessionKeepsItsLatestLeaf(t *testing.T) {
	dir := t.TempDir()
	queue := offloop.New()
	defer queue.Stop()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir, WriteQueue: queue})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	queue.Flush()

	release := make(chan struct{})
	queue.Go(func() { <-release })
	manager.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "kept answer"}}, StopReason: ai.StopStop,
	})
	wiring := &SessionWiring{SessionInfo: manager}
	var renameErr error
	wiring.renameSessionFile(manager.GetSessionFile(), "renamed", func(err error) { renameErr = err })
	if renameErr != nil {
		t.Fatal(renameErr)
	}
	if name := manager.GetSessionName(); name != "renamed" {
		t.Fatalf("active session name = %q, want the rename to update its own manager", name)
	}
	close(release)
	queue.Flush()

	reopened, err := coding.OpenSession(manager.GetSessionFile(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if name := reopened.GetSessionName(); name != "renamed" {
		t.Fatalf("session name = %q", name)
	}
	for _, entry := range reopened.GetBranch("") {
		if entry.Message == nil {
			continue
		}
		message, err := ai.UnmarshalMessage(entry.Message)
		if err != nil {
			t.Fatal(err)
		}
		if ai.ContentText(ai.StringOrBlocks{Blocks: assistantContent(message)}, "") == "kept answer" {
			return
		}
	}
	t.Fatal("the queued assistant message was left off the reopened branch")
}

// A rename of a session that is not active still writes through a manager
// opened from its file.
func TestRenamingAnotherSessionStillWrites(t *testing.T) {
	targetDir := t.TempDir()
	target := coding.NewSessionManager(targetDir, &coding.SessionManagerOptions{SessionDir: targetDir})
	target.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "other"}})
	currentDir := t.TempDir()
	current := coding.NewSessionManager(currentDir, &coding.SessionManagerOptions{SessionDir: currentDir})
	wiring := &SessionWiring{SessionInfo: current}
	renameErr := error(nil)
	wiring.renameSessionFile(target.GetSessionFile(), "other session", func(err error) { renameErr = err })
	if renameErr != nil {
		t.Fatal(renameErr)
	}
	reopened, err := coding.OpenSession(target.GetSessionFile(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if name := reopened.GetSessionName(); name != "other session" {
		t.Fatalf("renamed name = %q", name)
	}
}

func assistantContent(message ai.Message) ai.ContentList {
	if assistant, ok := message.(*ai.AssistantMessage); ok {
		return assistant.Content
	}
	return nil
}
