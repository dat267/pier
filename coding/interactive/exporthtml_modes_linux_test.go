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

func TestWriteDebugLogFileUsesNodeDefaultModes(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(dir, "agent"))
	if err := WriteDebugLogFile("debug output"); err != nil {
		t.Fatal(err)
	}
	debugPath := filepath.Join(dir, "agent", coding.AppName+"-debug.log")
	fileInfo, err := os.Stat(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o666 {
		t.Fatalf("debug file mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(filepath.Dir(debugPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("debug directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

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
