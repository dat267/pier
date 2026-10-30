package coding

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), core/cache-warmer.ts lifecycle methods
// snapshot a run and stop synchronously on one event loop. Go host callbacks
// can interleave a replacement, which must remain untouched by stale events.
func TestCacheWarmerStaleLifecycleEventCannotStopReplacement(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		mode     CacheWarmingMode
		dispatch func(*CacheWarmer)
	}{
		{name: "agent-settled", mode: CacheWarmingStreaming, dispatch: (*CacheWarmer).OnAgentSettled},
		{name: "mode-changed", mode: CacheWarmingOff, dispatch: (*CacheWarmer).OnModeChanged},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			model := warmerModel()
			model.PromptCache[ai.CacheRetentionShort] = 3600
			warmer, manager, _ := newTestWarmer(t, model, CacheWarmingIdle)
			defer warmer.Cancel()
			manager.AppendMessage(&ai.AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
			})
			warmer.Start(CacheWarmRequest{Model: model}, func() bool { return true })

			entered := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			warmer.GetMode = func() CacheWarmingMode {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
					return fixture.mode
				}
				return CacheWarmingIdle
			}
			done := make(chan struct{})
			go func() {
				fixture.dispatch(warmer)
				close(done)
			}()
			<-entered

			// The lifecycle callback was admitted for the old run, but a new real
			// request replaced it before the host mode callback returned.
			warmer.Start(CacheWarmRequest{Model: model}, func() bool { return true })
			close(release)
			<-done
			if status := warmer.Status(); status.State != "scheduled" {
				t.Fatalf("stale %s event stopped its replacement: %+v", fixture.name, status)
			}
		})
	}
}

// pi v1.4.0 (4a080252), core/cache-warmer.ts validateRun/stop: the
// `this.run !== run` check and the stop are atomic because JS has no await
// between them. A stale Go refresh must therefore never stop its replacement.
func TestCacheWarmerStaleRunCannotStopReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		model := warmerModel()
		warmer, manager, _ := newTestWarmer(t, model, CacheWarmingIdle)
		defer warmer.Cancel()
		manager.AppendMessage(&ai.AssistantMessage{
			API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
			Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
		})

		entered := make(chan struct{})
		release := make(chan struct{})
		var currentCalls atomic.Int32
		warmer.Start(CacheWarmRequest{Model: model}, func() bool {
			if currentCalls.Add(1) == 1 {
				close(entered)
				<-release
				return false
			}
			return true
		})
		time.Sleep(1100 * time.Millisecond)
		<-entered

		// A real request replaced the run while the stale refresh was inside its
		// currency check.
		warmer.Start(CacheWarmRequest{Model: model}, func() bool { return true })
		close(release)
		synctest.Wait()

		status := warmer.Status()
		if status.State != "scheduled" {
			t.Fatalf("stale run stopped its replacement: %+v", status)
		}
	})
}
