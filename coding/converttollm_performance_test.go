package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

func convertToLlmLargeInput() []ai.Message {
	messages := make([]ai.Message, 0, 40000)
	for i := 0; i < 10000; i++ {
		messages = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
		messages = append(messages, &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop})
		messages = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("x", 20)}})
		messages = append(messages, &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop})
	}
	return messages
}

func TestConvertToLlmAllocationBudget(t *testing.T) {
	messages := convertToLlmLargeInput()
	allocs := testing.AllocsPerRun(3, func() { _ = ConvertToLlm(messages) })
	if allocs > 12000 {
		t.Fatalf("ConvertToLlm allocated %.0f objects, want <= 12000", allocs)
	}
}

func BenchmarkConvertToLlmLarge(b *testing.B) {
	messages := convertToLlmLargeInput()
	b.ReportAllocs()
	for b.Loop() {
		_ = ConvertToLlm(messages)
	}
}
