package interactive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// D371: a failed manual Go copy must not leave its newly created session file.
// Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/agent-session-runtime.ts
// importFromJsonl delegates to Node's copyFileSync/COPYFILE_EXCL. This pins the
// Go error path, not a claim that Node rejects this directory input.
func TestImportCopyFailureDoesNotLeaveAPartialSession(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "fresh destination"
		if existing {
			name = "existing name preserved"
		}
		t.Run(name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			sessionDir := withSessionDir(t, app)
			previousID := app.sessionMgr.GetSessionID()
			source := filepath.Join(t.TempDir(), "broken.jsonl")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(sessionDir, filepath.Base(source))
			if existing {
				if err := os.WriteFile(original, []byte("keep this session"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			newCommandWiring(app).HandleImportCommand(context.Background(), "/import "+source)
			answerConfirm(t, app, 0)
			if !transcriptContains(app, "Failed to import session:") {
				t.Fatal("copy failure was not reported")
			}
			if app.sessionMgr.GetSessionID() != previousID {
				t.Fatal("failed copy replaced the active session")
			}
			entries, err := os.ReadDir(sessionDir)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if existing {
				want = 1
				data, err := os.ReadFile(original)
				if err != nil || string(data) != "keep this session" {
					t.Fatalf("existing session changed: %q, err=%v", data, err)
				}
			}
			if len(entries) != want {
				t.Fatalf("session files after failed copy = %v, want %d existing files only", entries, want)
			}
		})
	}
}
