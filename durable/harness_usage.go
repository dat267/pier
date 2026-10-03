package durable

import (
	"context"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/usage.ts: the ledger of one conversation's own spend.

// UsageState is the `pi.usage` document value: assistant entries and
// summarization attempts keyed `provider/modelId`, and tool results keyed by
// tool name.
type UsageState struct {
	Models map[string]ai.Usage `json:"models"`
	Tools  map[string]ai.Usage `json:"tools"`
}

// Usage bucket names.
const (
	UsageBucketModels = "models"
	UsageBucketTools  = "tools"
)

// UsageDoc is the conversation-scoped usage document (latest history, initial
// fork).
var UsageDoc = mustDefineDoc(DocDefinition{
	Kind: "pi.usage", Version: 1, Scope: ScopeConversation,
	History: stringPointer(HistoryLatest), Fork: stringPointer(ForkInitial),
	Initial: func(seed chord.JsonValue) (chord.JsonValue, error) {
		return map[string]any{"models": map[string]any{}, "tools": map[string]any{}}, nil
	},
	CheckpointWhen: func(value chord.JsonValue, ops []delta.Op, info CheckpointInfo) bool { return true },
})

// mustDefineDoc validates a document definition at package init.
func mustDefineDoc(definition DocDefinition) DocToken {
	token, err := DefineDoc(definition)
	if err != nil {
		panic(err)
	}
	return token
}

// RecordUsage adds usage to one bucket of the conversation's `pi.usage` in the
// commit that records the response.
func RecordUsage(ctx context.Context, tx *Transaction, conversationID Id, bucket string, key string, usage ai.Usage) error {
	draft, err := tx.Doc(UsageDoc.Definition, conversationID)
	if err != nil {
		return err
	}
	state, err := decodeUsageState(draft)
	if err != nil {
		return err
	}
	table := state.Models
	if bucket == UsageBucketTools {
		table = state.Tools
	}
	if existing, present := table[key]; present {
		AddUsage(&existing, usage)
		table[key] = existing
	} else {
		table[key] = usage
	}
	AssignJSON(draft.(map[string]any), bucket, usageTableValue(table))
	return nil
}

// AddUsage adds every counter of usage to total; optional counters are added
// once either side reports them.
func AddUsage(total *ai.Usage, usage ai.Usage) {
	total.Input += usage.Input
	total.Output += usage.Output
	total.CacheRead += usage.CacheRead
	total.CacheWrite += usage.CacheWrite
	total.TotalTokens += usage.TotalTokens
	if usage.CacheWrite1h != nil {
		current := int64(0)
		if total.CacheWrite1h != nil {
			current = *total.CacheWrite1h
		}
		sum := current + *usage.CacheWrite1h
		total.CacheWrite1h = &sum
	}
	if usage.Reasoning != nil {
		current := int64(0)
		if total.Reasoning != nil {
			current = *total.Reasoning
		}
		sum := current + *usage.Reasoning
		total.Reasoning = &sum
	}
	total.Cost.Input += usage.Cost.Input
	total.Cost.Output += usage.Cost.Output
	total.Cost.CacheRead += usage.Cost.CacheRead
	total.Cost.CacheWrite += usage.Cost.CacheWrite
	total.Cost.Total += usage.Cost.Total
}

// AddUsageState adds every bucket of state into sum.
func AddUsageState(sum *UsageState, state UsageState) {
	for _, bucket := range []string{UsageBucketModels, UsageBucketTools} {
		target := sum.Models
		source := state.Models
		if bucket == UsageBucketTools {
			target = sum.Tools
			source = state.Tools
		}
		if target == nil {
			target = map[string]ai.Usage{}
		}
		for key, usage := range source {
			if existing, present := target[key]; present {
				AddUsage(&existing, usage)
				target[key] = existing
			} else {
				target[key] = usage
			}
		}
		if bucket == UsageBucketModels {
			sum.Models = target
		} else {
			sum.Tools = target
		}
	}
}

// decodeUsageState converts the draft JSON into a typed state.
func decodeUsageState(draft chord.JsonValue) (UsageState, error) {
	state := UsageState{Models: map[string]ai.Usage{}, Tools: map[string]ai.Usage{}}
	encoded, err := marshalJSONValue(draft)
	if err != nil {
		return state, err
	}
	if err := unmarshalBytes([]byte(encoded), &state); err != nil {
		return state, err
	}
	if state.Models == nil {
		state.Models = map[string]ai.Usage{}
	}
	if state.Tools == nil {
		state.Tools = map[string]ai.Usage{}
	}
	return state, nil
}

// usageTableValue renders one bucket as a JSON object.
func usageTableValue(table map[string]ai.Usage) chord.JsonValue {
	encoded, err := marshalJSONValue(table)
	if err != nil {
		return map[string]any{}
	}
	var value chord.JsonValue
	if err := unmarshalBytes([]byte(encoded), &value); err != nil {
		return map[string]any{}
	}
	return value
}

// stringPointer is the non-test optional-string helper.
func stringPointer(value string) *string { return &value }
