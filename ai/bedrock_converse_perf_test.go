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
