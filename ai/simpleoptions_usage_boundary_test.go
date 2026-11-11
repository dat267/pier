package ai

import "testing"

// Reference: pi v1.5.2 (b4afdddf), api/simple-options.ts
// clampMaxTokensToContext subtracts the usage and the safety margin, which is
// the larger of 4096 tokens and 15% of the counted input, as Numbers.
// D313 saturates an oversized usage estimate at Go's int bound, but reserving
// the margin must not wrap it back into positive answer room.
func TestClampMaxTokensToContextKeepsHugeUsageOutOfAnswerRoom(t *testing.T) {
	context := TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Usage:      Usage{TotalTokens: int64(^uint64(0) >> 1)},
			StopReason: StopStop,
		},
	}}
	for _, window := range []int64{2048, 128000} {
		model := &Model{ContextWindow: window}
		if got := ClampMaxTokensToContext(model, context, 1024); got != 1 {
			t.Errorf("window %d: max tokens = %d, want 1 for an overfull context", window, got)
		}
	}
	model := &Model{ContextWindow: 8192}
	if got := ClampMaxTokensToContext(model, TranscriptContext{}, 1024); got != 1024 {
		t.Errorf("empty context: max tokens = %d, want 1024", got)
	}
}
