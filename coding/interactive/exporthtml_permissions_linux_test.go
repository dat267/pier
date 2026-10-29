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

// pi v1.4.0 packages/coding-agent/src/core/export-html/index.ts writes exports
// with the process umask; Pier keeps sensitive transcript exports private.
func TestExportHTMLWritesPrivateFiles(t *testing.T) {
	oldUmask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	for _, export := range []struct {
		name string
		run  func(*coding.AgentSession, string, string) error
	}{
		{
			name: "from-file",
			run: func(_ *coding.AgentSession, sessionPath, outputPath string) error {
				_, err := ExportFromFile(sessionPath, outputPath, "dark")
				return err
			},
		},
		{
			name: "live-session",
			run: func(session *coding.AgentSession, _, outputPath string) error {
				_, err := ExportSessionToHTML(session, outputPath, "dark")
				return err
			},
		},
	} {
		for _, existing := range []bool{false, true} {
			name := "new-file"
			if existing {
				name = "existing-file"
			}
			t.Run(export.name+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir})
				manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "private transcript"}})
				sessionPath := manager.GetSessionFile()

				session, err := coding.NewAgentSession(&coding.SessionConfig{
					Cwd:   dir,
					Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
					StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
						return nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Dispose()
				session.Sessions = manager

				outputPath := filepath.Join(dir, "export.html")
				if existing {
					if err := os.WriteFile(outputPath, []byte("old export"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := export.run(session, sessionPath, outputPath); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(outputPath)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != 0o600 {
					t.Fatalf("export mode = %#o, want %#o", got, 0o600)
				}
			})
		}
	}
}
