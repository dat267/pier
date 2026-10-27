package interactive

import (
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// Reference: pi v1.4.0 (4a080252), interactive-mode.ts renderInitialMessages
// counts all persisted compactions, not just the active branch. The Go port
// need not materialize a full descriptor snapshot to compute that number.
func TestTranscriptCompactionNoticeDoesNotCopyHistory(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: boolPtr(false)})
	last := ""
	for i := 0; i < 512; i++ {
		last = manager.AppendCustomEntry("hidden", nil)
	}
	manager.AppendCompaction("old branch", last, 100, nil, false, nil)
	if err := manager.Branch(last); err != nil {
		t.Fatal(err)
	}
	manager.AppendCompaction("current branch", last, 100, nil, false, nil)
	manager.Projection()
	renderer.SessionInfo = manager
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	renderer.RenderInitialMessages()
	runtime.ReadMemStats(&after)
	// The active window is tiny. Allow component construction, but not a
	// roughly 225 KiB full-history snapshot just to count two compactions.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64*1024 {
		t.Fatalf("replaying a compacted window allocated %d bytes, want at most 65536", allocated)
	}
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	if !strings.Contains(lines, "Session compacted 2 times") {
		t.Fatalf("off-branch compaction was not counted: %s", lines)
	}
}
