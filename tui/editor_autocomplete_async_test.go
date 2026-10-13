package tui

import (
	"context"
	"sync"
	"testing"
	"time"
)

// loopHost simulates the UI owner loop: Post queues callbacks that the test
// drains explicitly, so a provider result is applied only when the owner runs.
type loopHost struct {
	mu      sync.Mutex
	posts   []func()
	posted  chan struct{}
	renders int
}

func newLoopHost() *loopHost { return &loopHost{posted: make(chan struct{}, 64)} }

func (h *loopHost) Rows() int { return 10 }

func (h *loopHost) RequestRender(force bool) {
	h.mu.Lock()
	h.renders++
	h.mu.Unlock()
}

func (h *loopHost) Post(fn func()) {
	h.mu.Lock()
	h.posts = append(h.posts, fn)
	h.mu.Unlock()
	select {
	case h.posted <- struct{}{}:
	default:
	}
}

func (h *loopHost) drain() {
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

func (h *loopHost) waitPost(t *testing.T) {
	t.Helper()
	select {
	case <-h.posted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a posted callback")
	}
}

// scriptedProvider returns a fixed suggestion set, optionally blocking until the
// test releases it so the request can be observed mid-flight.
type scriptedProvider struct {
	mu          sync.Mutex
	calls       int
	contexts    []context.Context
	started     chan struct{}
	release     chan struct{}
	suggestions *AutocompleteSuggestions
}

func (p *scriptedProvider) TriggerCharacters() []string { return nil }

func (p *scriptedProvider) GetSuggestions(ctx context.Context, lines []string, cursorLine int, cursorCol int, force bool) *AutocompleteSuggestions {
	p.mu.Lock()
	p.calls++
	p.contexts = append(p.contexts, ctx)
	p.mu.Unlock()
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	if p.release != nil {
		<-p.release
	}
	return p.suggestions
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *scriptedProvider) firstContext() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.contexts) == 0 {
		return nil
	}
	return p.contexts[0]
}

func (p *scriptedProvider) ApplyCompletion(lines []string, cursorLine int, cursorCol int, item AutocompleteItem, prefix string) CompletionResult {
	return CompletionResult{Lines: lines, CursorLine: cursorLine, CursorCol: cursorCol}
}

type stubTimer struct{}

func (stubTimer) Stop() bool { return false }

// TestEditorAutocompleteRunsOffLoop follows v1.2.0 editor.ts
// requestAutocomplete/startAutocompleteRequest/runAutocompleteRequest: the
// provider runs off the owner loop and its result is applied through the
// owner's Post, never synchronously under the editor.
func TestEditorAutocompleteRunsOffLoop(t *testing.T) {
	saved := autocompleteDebounceDelay
	autocompleteDebounceDelay = 0
	defer func() { autocompleteDebounceDelay = saved }()

	host := newLoopHost()
	editor := NewEditor(host, editorTestTheme(), EditorOptions{})
	provider := &scriptedProvider{
		started:     make(chan struct{}, 4),
		release:     make(chan struct{}),
		suggestions: &AutocompleteSuggestions{Prefix: "@", Items: []AutocompleteItem{{Value: "@a"}}},
	}
	editor.SetAutocompleteProvider(provider)

	editor.HandleInput("@")
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not called off-loop")
	}
	if editor.IsShowingAutocomplete() {
		t.Fatal("suggestions were applied before the owner loop drained the post")
	}

	close(provider.release)
	host.waitPost(t)
	host.drain()
	if !editor.IsShowingAutocomplete() {
		t.Fatal("suggestions were not applied on the owner loop")
	}
}

// TestEditorAutocompleteDebouncesTriggerTyping follows editor.ts
// getAutocompleteDebounceMs: a non-forced request for a trigger token waits for
// the debounce timer instead of calling the provider immediately.
func TestEditorAutocompleteDebouncesTriggerTyping(t *testing.T) {
	savedAfter, savedDelay := autocompleteAfter, autocompleteDebounceDelay
	defer func() { autocompleteAfter, autocompleteDebounceDelay = savedAfter, savedDelay }()
	autocompleteDebounceDelay = 20 * time.Millisecond
	var scheduled func()
	autocompleteAfter = func(d time.Duration, fn func()) autocompleteTimer {
		scheduled = fn
		return stubTimer{}
	}

	host := newLoopHost()
	editor := NewEditor(host, editorTestTheme(), EditorOptions{})
	provider := &scriptedProvider{
		started:     make(chan struct{}, 4),
		suggestions: &AutocompleteSuggestions{Prefix: "@", Items: []AutocompleteItem{{Value: "@a"}}},
	}
	editor.SetAutocompleteProvider(provider)

	editor.HandleInput("@")
	if provider.callCount() != 0 {
		t.Fatal("the provider ran before the debounce timer fired")
	}
	if scheduled == nil {
		t.Fatal("no debounce was scheduled for a trigger token")
	}

	scheduled()
	host.waitPost(t)
	host.drain()
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not called after the debounce fired")
	}
	host.waitPost(t)
	host.drain()
	if !editor.IsShowingAutocomplete() {
		t.Fatal("suggestions were not applied after the debounced request")
	}
}

// TestEditorAutocompleteDebounceDecision pins the force/explicit-Tab bypass and
// the trigger-token gate of getAutocompleteDebounceMs.
func TestEditorAutocompleteDebounceDecision(t *testing.T) {
	host := newLoopHost()
	editor := NewEditor(host, editorTestTheme(), EditorOptions{})
	editor.SetText("@src")
	if got := editor.getAutocompleteDebounceDelayLocked(false, false); got != autocompleteDebounceDelay {
		t.Errorf("trigger token delay = %v, want %v", got, autocompleteDebounceDelay)
	}
	if got := editor.getAutocompleteDebounceDelayLocked(false, true); got != 0 {
		t.Errorf("explicit tab delay = %v, want 0", got)
	}
	if got := editor.getAutocompleteDebounceDelayLocked(true, true); got != 0 {
		t.Errorf("forced delay = %v, want 0", got)
	}
	editor.SetText("/model")
	if got := editor.getAutocompleteDebounceDelayLocked(false, false); got != 0 {
		t.Errorf("slash-command delay = %v, want 0", got)
	}
}

// TestEditorAutocompleteDropsStaleResults follows editor.ts
// isAutocompleteRequestCurrent: a superseded request's late result is discarded
// and its AbortSignal is aborted.
func TestEditorAutocompleteDropsStaleResults(t *testing.T) {
	saved := autocompleteDebounceDelay
	autocompleteDebounceDelay = 0
	defer func() { autocompleteDebounceDelay = saved }()

	host := newLoopHost()
	editor := NewEditor(host, editorTestTheme(), EditorOptions{})
	releaseFirst := make(chan struct{})
	first := &scriptedProvider{
		started:     make(chan struct{}, 1),
		release:     releaseFirst,
		suggestions: &AutocompleteSuggestions{Prefix: "@a", Items: []AutocompleteItem{{Value: "@a"}}},
	}
	editor.SetAutocompleteProvider(first)
	editor.HandleInput("@")
	<-first.started

	// A second request supersedes the first: cancel the in-flight one and start
	// another. The first provider is still blocked.
	second := &scriptedProvider{
		started:     make(chan struct{}, 1),
		suggestions: &AutocompleteSuggestions{Prefix: "@ab", Items: []AutocompleteItem{{Value: "@abc"}}},
	}
	editor.SetAutocompleteProvider(second)
	editor.HandleInput("b")
	select {
	case <-second.started:
	case <-time.After(5 * time.Second):
		t.Fatal("second provider was not called")
	}
	if ctx := first.firstContext(); ctx == nil || ctx.Err() == nil {
		t.Fatal("the superseded request's context was not canceled")
	}

	host.waitPost(t)
	host.drain()
	if editor.autocompletePrefix != "@ab" {
		t.Fatalf("prefix = %q, want the second request's @ab", editor.autocompletePrefix)
	}

	// The first request finishing now must not overwrite the second result.
	close(releaseFirst)
	host.waitPost(t)
	host.drain()
	if editor.autocompletePrefix != "@ab" {
		t.Fatalf("stale result overwrote prefix: %q", editor.autocompletePrefix)
	}
}
