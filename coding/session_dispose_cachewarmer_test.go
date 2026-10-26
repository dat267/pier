package coding

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/agent-session.ts dispose clears onWarmed and
// cancels its cache warmer. A disposed session must not issue future paid
// refreshes or retain its session event callback. The clock is virtual.
func TestDisposeStopsSessionCacheWarming(t *testing.T) {
	for _, dispose := range []bool{false, true} {
		name := "live-control"
		if dispose {
			name = "disposed"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				model := warmerModel()
				warmer, manager, runs := newTestWarmer(t, model, CacheWarmingIdle)
				defer warmer.Cancel()
				manager.AppendMessage(&ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
				})
				session, err := NewAgentSession(&SessionConfig{Cwd: manager.GetCwd(), Sessions: manager, Model: model, StreamFn: stubStreamFn})
				if err != nil {
					t.Fatal(err)
				}
				session.CacheWarmer = warmer
				var notifications atomic.Int64
				warmer.OnWarmed = func(*SessionEntry) { notifications.Add(1) }
				warmer.Decide = func(CacheWarmingDecisionEvent) CacheWarmingAction { return CacheWarmingActionWarm }
				warmer.Start(CacheWarmRequest{Model: model, Options: &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{Ctx: context.Background()}}}, func() bool { return true })
				if status := warmer.Status(); status.State != "scheduled" {
					t.Fatalf("warm run was not scheduled: %+v", status)
				}
				if dispose {
					session.Dispose()
					if status := warmer.Status(); status.State != "inactive" {
						t.Errorf("disposed warmer = %+v, want inactive", status)
					}
				}
				time.Sleep(1500 * time.Millisecond)
				synctest.Wait()
				want := int64(1)
				if dispose {
					want = 0
				}
				if got := runs.Load(); got != want {
					t.Errorf("cache refresh requests = %d, want %d", got, want)
				}
				if got := notifications.Load(); got != want {
					t.Errorf("warm notifications = %d, want %d", got, want)
				}
				if dispose && warmer.OnWarmed != nil {
					t.Error("disposed warmer retained its session callback")
				}
			})
		})
	}
}
