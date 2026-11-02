package coding

// Benchmarks for the footer's per-event session reads. The window, its token
// estimate and the newest compaction are cached by branch version, so a long
// session's paints must stay far below the walk cost they used to pay (M139).

import (
	"testing"

	"github.com/dat267/pier/ai"
)

func benchmarkUsageSession(b *testing.B, pairs int) *AgentSession {
	b.Helper()
	manager := NewSessionManager("/tmp", &SessionManagerOptions{Persist: new(bool)})
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 4096}
	session := newControlSession(b, model, nil)
	session.Sessions = manager
	for i := 0; i < pairs; i++ {
		manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question text here"}})
		manager.AppendMessage(&ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
			Content: ai.ContentList{ai.TextContent{Text: "answer text here"}}, StopReason: ai.StopStop,
		})
	}
	return session
}

func BenchmarkGetContextUsage(b *testing.B) {
	session := benchmarkUsageSession(b, 4000)
	_ = session.GetContextUsage()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = session.GetContextUsage()
	}
}

func BenchmarkProjectedEntries(b *testing.B) {
	session := benchmarkUsageSession(b, 4000)
	_ = session.Sessions.ProjectedEntries()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = session.Sessions.ProjectedEntries()
	}
}

func BenchmarkLatestCompaction(b *testing.B) {
	session := benchmarkUsageSession(b, 4000)
	_ = session.Sessions.LatestCompaction()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = session.Sessions.LatestCompaction()
	}
}
