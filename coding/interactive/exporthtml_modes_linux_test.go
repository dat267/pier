//go:build linux

package interactive

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

func TestExportSessionToHTMLUsesNodeDefaultFileMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	root := t.TempDir()
	manager := coding.NewSessionManager(root, &coding.SessionManagerOptions{Persist: boolPtr(true)})
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	agentSession, err := coding.NewAgentSession(&coding.SessionConfig{
		Cwd: root, Model: model,
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	agentSession.Sessions = manager
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "export"}})
	appSession := &AppSession{AgentSession: agentSession}
	path := filepath.Join(root, "export.html")
	if _, err := appSession.ExportSessionToHTML(path, "dark"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("HTML export mode = %#o; want Node default %#o", got, 0o666)
	}

	fileExport := filepath.Join(root, "file-export.html")
	if _, err := ExportFromFile(manager.GetSessionFile(), fileExport, "dark"); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(fileExport)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o666 {
		t.Fatalf("file-export mode = %#o; want Node default %#o", got, 0o666)
	}
}
