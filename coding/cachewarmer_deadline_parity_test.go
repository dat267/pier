package coding

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/cache-warmer.ts schedule/refresh: allow half
// the pre-expiry margin for timer and decision delays, then stop rather than
// risk a full-price cache write. For an 11s TTL, delay=1s and deadline=6s.
func TestCacheWarmerSkipsRefreshPastDispatchDeadline(t *testing.T) {
	for _, fixture := range []struct {
		name                        string
		modeDelay, decisionDelay    time.Duration
		wantRequests, wantDecisions int64
	}{
		{"on-time", 0, 0, 1, 1},
		{"deadline-control", 0, 5 * time.Second, 1, 1},
		{"late-dispatch", 6 * time.Second, 0, 0, 0},
		{"late-decision", 0, 6 * time.Second, 0, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				model := warmerModel()
				warmer, manager, requests := newTestWarmer(t, model, CacheWarmingIdle)
				defer warmer.Cancel()
				manager.AppendMessage(&ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
				})
				var modes, decisions atomic.Int64
				warmer.GetMode = func() CacheWarmingMode {
					if modes.Add(1) == 2 {
						time.Sleep(fixture.modeDelay)
					}
					return CacheWarmingIdle
				}
				warmer.Decide = func(CacheWarmingDecisionEvent) CacheWarmingAction {
					decisions.Add(1)
					time.Sleep(fixture.decisionDelay)
					return CacheWarmingActionWarm
				}
				warmer.Start(CacheWarmRequest{Model: model, Options: &ai.SimpleStreamOptions{CacheRetention: ai.CacheRetentionShort}}, func() bool { return true })
				time.Sleep(time.Second + fixture.modeDelay + fixture.decisionDelay + time.Millisecond)
				synctest.Wait()
				if got := requests.Load(); got != fixture.wantRequests {
					t.Errorf("refresh requests = %d, want %d", got, fixture.wantRequests)
				}
				if got := decisions.Load(); got != fixture.wantDecisions {
					t.Errorf("policy decisions = %d, want %d", got, fixture.wantDecisions)
				}
				if fixture.wantRequests == 0 {
					if status := warmer.Status(); status.State != "inactive" || status.Reason != "cache refresh deadline missed" {
						t.Fatalf("late refresh status = %+v", status)
					}
				}
			})
		})
	}
}
