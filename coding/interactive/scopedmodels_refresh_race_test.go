package interactive

import (
	"context"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// pi 4a080252 components/scoped-models-selector.ts applies its refresh result
// on the JS event loop, so dismissal and the in-flight refresh never touch the
// same state concurrently. Pier's refresh worker reads the dismissal flag while
// the owner writes it; this exercises that handoff under -race.
func TestScopedModelsRefreshObservesDismissalSafely(t *testing.T) {
	models := []*ai.Model{{ID: "a", Provider: "p"}}
	finished := make(chan struct{})
	runtime := &modelTestRuntime{models: models, refresh: func() (ai.ModelsRefreshResult, error) {
		close(finished)
		return ai.ModelsRefreshResult{}, errors.New("offline")
	}}
	wiring, _, _ := newModelTestWiring(t, runtime)
	wiring.ShowModelsSelector(context.Background())
	<-finished
	// The refresh has settled, so the worker is about to re-read the dismissal
	// flag while the owner sets it. No channel orders those two accesses, so
	// -race reports the unsynchronized read.
	wiring.Slot.DisposeActiveSelector()
	for range 2 {
		wiring.UI.(*tui.MainScreen).RenderNow(true)
	}
}
