package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of harness/usage.ts.

func usageWithOptionals(cacheWrite1h, reasoning int64) ai.Usage {
	return ai.Usage{
		Input: 10, Output: 20, CacheRead: 5, CacheWrite: 7,
		CacheWrite1h: &cacheWrite1h, Reasoning: &reasoning, TotalTokens: 42,
		Cost: ai.UsageCost{Input: 0.1, Output: 0.2, CacheRead: 0.3, CacheWrite: 0.4, Total: 1.0},
	}
}

func TestAddUsage(t *testing.T) {
	oneHour, reasoning := int64(3), int64(4)
	total := usageWithOptionals(oneHour, reasoning)
	AddUsage(&total, ai.Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, TotalTokens: 5,
		Cost: ai.UsageCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 5}})
	if total.Input != 11 || total.Output != 22 || total.CacheRead != 8 || total.CacheWrite != 11 || total.TotalTokens != 47 {
		t.Fatalf("total = %+v", total)
	}
	if total.Cost.Input != 1.1 || total.Cost.Total != 6 {
		t.Fatalf("cost = %+v", total.Cost)
	}
	// The optional counters survive the side that omits them.
	if total.CacheWrite1h == nil || *total.CacheWrite1h != 3 || total.Reasoning == nil || *total.Reasoning != 4 {
		t.Fatalf("optional = %+v", total)
	}
	// A later optional count adds to the existing one.
	extra, reasoning2 := int64(2), int64(6)
	AddUsage(&total, ai.Usage{CacheWrite1h: &extra, Reasoning: &reasoning2})
	if *total.CacheWrite1h != 5 || *total.Reasoning != 10 {
		t.Fatalf("optional = %+v", total)
	}
	// An absent optional stays absent.
	fresh := ai.Usage{}
	AddUsage(&fresh, ai.Usage{Input: 1})
	if fresh.CacheWrite1h != nil || fresh.Reasoning != nil {
		t.Fatalf("fresh = %+v", fresh)
	}
}

func TestAddUsageState(t *testing.T) {
	sum := UsageState{Models: map[string]ai.Usage{}, Tools: map[string]ai.Usage{}}
	state := UsageState{
		Models: map[string]ai.Usage{"provider/model": {Input: 1, TotalTokens: 1}},
		Tools:  map[string]ai.Usage{"__proto__": {Input: 2, TotalTokens: 2}},
	}
	AddUsageState(&sum, state)
	AddUsageState(&sum, state)
	if sum.Models["provider/model"].Input != 2 || sum.Models["provider/model"].TotalTokens != 2 {
		t.Fatalf("models = %+v", sum.Models)
	}
	// A tool named __proto__ is an ordinary key (Go maps have no prototype).
	if sum.Tools["__proto__"].Input != 4 {
		t.Fatalf("tools = %+v", sum.Tools)
	}
}

func TestUsageDocRecordUsage(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	oneHour, reasoning := int64(1), int64(2)
	usage := usageWithOptionals(oneHour, reasoning)
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		return RecordUsage(context.Background(), tx, RootConversationID, UsageBucketModels, "provider/model", usage)
	}); err != nil {
		t.Fatal(err)
	}
	// A second response through the tools bucket adds independently.
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		return RecordUsage(context.Background(), tx, RootConversationID, UsageBucketTools, "bash", ai.Usage{Input: 1, TotalTokens: 1})
	}); err != nil {
		t.Fatal(err)
	}
	value, ok, err := session.Snapshot(context.Background(), UsageDoc.Definition, RootConversationID)
	if err != nil || !ok {
		t.Fatalf("snapshot = %v, %v", ok, err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var state UsageState
	if err := json.Unmarshal(encoded, &state); err != nil {
		t.Fatal(err)
	}
	if state.Models["provider/model"].Input != 10 || state.Models["provider/model"].TotalTokens != 42 {
		t.Fatalf("models = %+v", state.Models)
	}
	if state.Tools["bash"].Input != 1 || state.Tools["bash"].TotalTokens != 1 {
		t.Fatalf("tools = %+v", state.Tools)
	}
	// The document keeps the conversation fork semantics.
	if HistoryLatest != *UsageDoc.Definition.History || ForkInitial != *UsageDoc.Definition.Fork {
		t.Fatalf("semantics = %+v", UsageDoc.Definition)
	}
}
