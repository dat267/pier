package interactive

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// postRecorderHost records posted closures so the test can drain them on its
// own goroutine, standing in for the real owner loop.
type postRecorderHost struct {
	mu    sync.Mutex
	posts []func()
}

func (h *postRecorderHost) RequestRender(force bool) {}

func (h *postRecorderHost) Post(fn func()) {
	h.mu.Lock()
	h.posts = append(h.posts, fn)
	h.mu.Unlock()
}

func (h *postRecorderHost) pending() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.posts)
}

func (h *postRecorderHost) drain() {
	for {
		h.mu.Lock()
		posts := h.posts
		h.posts = nil
		h.mu.Unlock()
		if len(posts) == 0 {
			return
		}
		for _, fn := range posts {
			fn()
		}
	}
}

// TestEditPreviewAppliesThroughOwnerPost pins the edit preview's owner-loop
// contract: computeEditsPreview runs off-loop, but the component mutation (and
// its invalidate) is delivered through the host's Post, not applied from the
// worker. Applying directly raced ToolExecutionComponent.updateDisplay (found
// by TestEditToolRenderUpstreamParity under -race).
func TestEditPreviewAppliesThroughOwnerPost(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	host := &postRecorderHost{}
	component := NewToolExecutionComponent("edit", "call-1",
		map[string]any{
			"path":  file,
			"edits": []any{map[string]any{"oldText": "two", "newText": "TWO"}},
		},
		ToolExecutionOptions{}, &editRenderers, host, dir)
	component.SetArgsComplete()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && host.pending() == 0 {
		time.Sleep(time.Millisecond)
	}
	if host.pending() == 0 {
		t.Fatal("edit preview worker never posted its result")
	}

	state, ok := component.rendererState.(*editCallComponent)
	if !ok {
		t.Fatalf("renderer state = %T, want *editCallComponent", component.rendererState)
	}
	if state.snapshotPreview() != nil {
		t.Fatal("preview mutated the component before the owner loop drained the post")
	}
	host.drain()
	if state.snapshotPreview() == nil {
		t.Fatal("preview was not published when the owner loop drained the post")
	}
}
