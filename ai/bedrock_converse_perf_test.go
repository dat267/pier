package ai

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 bedrock-converse-stream.ts accumulates text into a
// JavaScript string; appends are cheap and the message sees a rope. A Go
// string += copies the whole prefix per delta, so the port uses a Builder and
// materializes snapshots without copying (D355).
func BenchmarkBedrockTextStream(b *testing.B) {
	const total = 100 << 10
	chunk := strings.Repeat("x", 16)
	block := &bedrockBlock{kind: "text"}
	blocks := []*bedrockBlock{block}
	output := &AssistantMessage{}
	b.ReportAllocs()
	b.SetBytes(total)
	for b.Loop() {
		for written := 0; written < total; written += len(chunk) {
			block.text.WriteString(chunk)
			syncBedrockContent(output, blocks)
		}
	}
}

// The builder returns no-copy snapshots; a later delta must not change text
// already published in an earlier partial event (D355).
func TestBedrockStreamingSnapshotsAreStable(t *testing.T) {
	block := &bedrockBlock{kind: "text"}
	block.text.WriteString("first")
	first := block.content().(TextContent).Text
	block.text.WriteString(" second")
	if first != "first" {
		t.Fatalf("earlier text snapshot changed to %q", first)
	}
	if got := block.content().(TextContent).Text; got != "first second" {
		t.Fatalf("current text = %q", got)
	}
	thinking := &bedrockBlock{kind: "thinking"}
	thinking.thinking.WriteString("one")
	oldThinking := thinking.content().(ThinkingContent).Thinking
	thinking.thinking.WriteString(" two")
	if oldThinking != "one" {
		t.Fatalf("earlier thinking snapshot changed to %q", oldThinking)
	}
}
