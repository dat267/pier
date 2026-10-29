package ai

import (
	"context"
	"strings"
	"testing"
)

// pi v1.4.0 google-generative-ai.ts consumes the SDK's event stream; its SSE
// reader accepts CR, LF, and CRLF line boundaries.
func TestGoogleChunkParserAcceptsCROnlyEventBoundaries(t *testing.T) {
	first := `{"candidates":[{"content":{"parts":[{"text":"one"}]}}]}`
	second := `{"candidates":[{"content":{"parts":[{"text":"two"}]}}]}`
	body := "data: " + first + "\r\rdata: " + second + "\r\r"
	var texts []string
	err := iterateGoogleChunks(context.Background(), strings.NewReader(body), func(chunk *googleChunk) {
		if len(chunk.Candidates) != 0 && chunk.Candidates[0].Content != nil && len(chunk.Candidates[0].Content.Parts) != 0 {
			texts = append(texts, chunk.Candidates[0].Content.Parts[0].Text)
		}
	})
	if err != nil {
		t.Fatalf("iterateGoogleChunks returned error: %v", err)
	}
	if strings.Join(texts, ",") != "one,two" {
		t.Fatalf("parsed event texts = %q, want one,two", strings.Join(texts, ","))
	}
}
