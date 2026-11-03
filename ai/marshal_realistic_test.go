package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// realisticConversationMessages is the fair-baseline workload for M143:
// user/assistant pairs with model metadata, provider usage and cost, and
// timestamps. BenchmarkMarshalConversation keeps the older minimal 4k body,
// while this factory covers realistic request and session sizes.
func realisticConversationMessages(n int) []Message {
	messages := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			messages = append(messages, &UserMessage{
				Content: StringOrBlocks{Text: fmt.Sprintf(
					"Question %d with <html> & unicode café 😀, a path /tmp/file.go, and some `code`", i)},
				Timestamp: 1690000000000 + int64(i)*1000,
			})
			continue
		}
		messages = append(messages, &AssistantMessage{
			Content: ContentList{TextContent{Text: fmt.Sprintf(
				"Answer %d with **markdown**, a [link](https://example.com/%d), and unicode café 😀", i, i)}},
			API:      APIAnthropicMessages,
			Provider: "anthropic",
			Model:    "claude-sonnet-4-5",
			Usage: Usage{
				Input: 1200 + int64(i), Output: 240 + int64(i), CacheRead: 800, CacheWrite: 120,
				TotalTokens: 2360 + int64(i)*2,
				Cost:        UsageCost{Input: 0.0036, Output: 0.0036, CacheRead: 0.00024, CacheWrite: 0.00045, Total: 0.00789},
			},
			StopReason: StopStop,
			Timestamp:  1690000000000 + int64(i)*1000,
		})
	}
	return messages
}

func BenchmarkMarshalConversationRealistic(b *testing.B) {
	for _, n := range []int{100, 500, 4000} {
		messages := realisticConversationMessages(n)
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := MarshalJSON(messages); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestWriteRealisticConversationFixtures writes the Node benchmark inputs.
// It is skipped unless PIER_BENCH_JSON_DIR names an output directory.
// Run it with benchmark.mjs beside this file:
//
//	PIER_BENCH_JSON_DIR=/tmp/pier-bench go test ./ai -run TestWriteRealisticConversationFixtures
//	node ai/testdata/marshal-conversation/benchmark.mjs /tmp/pier-bench
func TestWriteRealisticConversationFixtures(t *testing.T) {
	dir := os.Getenv("PIER_BENCH_JSON_DIR")
	if dir == "" {
		t.Skip("set PIER_BENCH_JSON_DIR to write Node benchmark inputs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{100, 500, 4000} {
		encoded, err := MarshalJSON(realisticConversationMessages(n))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("realistic-%d.json", n))
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%d messages: %d bytes -> %s", n, len(encoded), path)
	}
}
