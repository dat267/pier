package coding

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/cache-warmer.ts refresh catches decision
// failures (fallback to economics) and refresh/notification failures (best
// effort, then reschedule). Isolate formerly fatal timer-goroutine panics.
func TestCacheWarmerCallbackFailuresAreBestEffort(t *testing.T) {
	const probeEnv = "PIER_TEST_CACHE_WARMER_CALLBACK_FAILURE"
	if mode := os.Getenv(probeEnv); mode != "" {
		synctest.Test(t, func(t *testing.T) {
			model := warmerModel()
			warmer, manager, _ := newTestWarmer(t, model, CacheWarmingIdle)
			defer warmer.Cancel()
			promptTokens := int64(100000)
			if mode == "decision-stop" {
				promptTokens = 1000
			}
			manager.AppendMessage(&ai.AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: promptTokens, TotalTokens: promptTokens},
			})
			var entered atomic.Bool
			var requests atomic.Int64
			warmer.Decide = func(CacheWarmingDecisionEvent) CacheWarmingAction {
				if strings.HasPrefix(mode, "decision-") {
					entered.Store(true)
					panic(errors.New("decision failed"))
				}
				return CacheWarmingActionWarm
			}
			stream := warmer.StreamSimple
			warmer.StreamSimple = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				requests.Add(1)
				if mode == "stream" {
					entered.Store(true)
					panic(errors.New("stream failed"))
				}
				return stream(model, transcript, options)
			}
			if mode == "notification" {
				warmer.OnWarmed = func(*SessionEntry) { entered.Store(true); panic(errors.New("notification failed")) }
			}
			warmer.Start(CacheWarmRequest{Model: model, Options: &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{CacheRetention: ai.CacheRetentionShort}}}, func() bool { return true })
			time.Sleep(1100 * time.Millisecond)
			synctest.Wait()
			if mode != "control" && !entered.Load() {
				t.Fatal("failure callback was not reached")
			}
			wantRequests := int64(1)
			if mode == "decision-stop" {
				wantRequests = 0
			}
			if got := requests.Load(); got != wantRequests {
				t.Fatalf("refresh attempts = %d, want %d", got, wantRequests)
			}
			status := warmer.Status()
			if mode == "decision-stop" {
				if status.State != "inactive" || status.Reason != "expected savings below threshold" {
					t.Fatalf("fallback status = %+v", status)
				}
			} else if status.State != "scheduled" {
				t.Fatalf("best-effort refresh did not reschedule: %+v", status)
			}
		})
		return
	}
	for _, mode := range []string{"control", "decision-warm", "decision-stop", "stream", "notification"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCacheWarmerCallbackFailuresAreBestEffort$", "-test.timeout=10s")
			command.Env = append(os.Environ(), probeEnv+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%s terminated its process: %v\n%s", mode, err, output)
			}
		})
	}
}
