package interactive

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D456: the reference registers a failed provider in a Map as each concurrent
// Promise.all refresh settles (packages/ai/src/models.ts:603), so the order it
// then lists is completion order. The port sorts instead, which is what every
// "Could not refresh …" message relies on.
func TestModelRefreshErrorProvidersAreSorted(t *testing.T) {
	failures := map[string]error{}
	for _, provider := range []string{"openai", "github-copilot", "openrouter", "anthropic", "google", "azure", "zai"} {
		failures[provider] = errors.New("refresh failed")
	}
	want := []string{"anthropic", "azure", "github-copilot", "google", "openai", "openrouter", "zai"}
	if got := errorProviders(failures); !slices.Equal(got, want) {
		t.Fatalf("providers = %q, want %q", got, want)
	}
	// The joined message is what the user sees; it must name every provider once.
	if joined := strings.Join(errorProviders(failures), ", "); joined != strings.Join(want, ", ") {
		t.Fatalf("joined = %q", joined)
	}
	if providers := errorProviders(nil); len(providers) != 0 {
		t.Fatalf("no failures = %q, want empty", providers)
	}
}

// Reference: pi v1.5.2 modes/interactive/model-catalog-refresh.ts aborts the
// shared refresh once its last waiter leaves, so no orphan fetch keeps running,
// and a caller arriving after a refresh finished starts a fresh one instead of
// observing the completed entry.
func TestCatalogRefreshAbandonsAndRestarts(t *testing.T) {
	runtime := newSharingCatalogRuntime(t)
	coordinator := &ModelCatalogRefreshCoordinator{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := coordinator.Refresh(ctx, runtime)
		done <- err
	}()
	<-runtime.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller err = %v", err)
	}
	select {
	case <-runtime.abandoned:
	case <-time.After(5 * time.Second):
		t.Fatal("shared refresh was not cancelled with its last waiter")
	}
	if waiters := coordinator.activeWaiters(runtime); waiters != 0 {
		t.Fatalf("waiters after cancel = %d", waiters)
	}

	// One token per fresh call: each caller starts its own refresh after the
	// cancelled entry was dropped.
	runtime.release <- struct{}{}
	runtime.release <- struct{}{}
	if _, err := coordinator.Refresh(context.Background(), runtime); err != nil {
		t.Fatalf("refresh after completion err = %v", err)
	}
	if _, err := coordinator.Refresh(context.Background(), runtime); err != nil {
		t.Fatalf("second refresh after completion err = %v", err)
	}
	if calls := runtime.calls.Load(); calls != 3 {
		t.Fatalf("refresh calls = %d, want the cancelled call plus two fresh ones", calls)
	}
}

// sharingCatalogRuntime counts refresh calls and reports cancellation.
type sharingCatalogRuntime struct {
	calls     atomic.Int64
	started   chan struct{}
	release   chan struct{}
	abandoned chan struct{}
}

func newSharingCatalogRuntime(t *testing.T) *sharingCatalogRuntime {
	t.Helper()
	return &sharingCatalogRuntime{
		started: make(chan struct{}, 8), release: make(chan struct{}, 8), abandoned: make(chan struct{}, 8),
	}
}

func (f *sharingCatalogRuntime) Refresh(ctx context.Context, _ *coding.ModelsRefreshCallOptions) (ai.ModelsRefreshResult, error) {
	f.calls.Add(1)
	f.started <- struct{}{}
	select {
	case <-f.release:
		return ai.ModelsRefreshResult{}, nil
	case <-ctx.Done():
		f.abandoned <- struct{}{}
		return ai.ModelsRefreshResult{}, ctx.Err()
	}
}
