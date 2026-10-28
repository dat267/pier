package ai

import (
	"context"
	"strings"
	"testing"
)

// pi v1.4.0 api/openai-responses-shared.ts backfillReasoningSignatures: Azure
// may supply reasoning encrypted_content only in the terminal response, so the
// merged signature must reach the persisted message (#6409).
func TestResponsesBackfillsEncryptedReasoningContent(t *testing.T) {
	run := func(events []string) *AssistantMessage {
		chunks := make([]string, 0, len(events))
		for _, event := range events {
			chunks = append(chunks, "data: "+event+"\n\n")
		}
		server := sseServer(t, chunks, nil)
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &OpenAIResponsesOptions{APIKey: "k"})
		msg, err := stream.Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	itemDone := `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]}}`
	added := `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`

	t.Run("signature-stringifies-the-decoded-item", func(t *testing.T) {
		msg := run([]string{
			added,
			`{"type":"response.output_item.done","output_index":0,"item":{ "type": "reasoning", "id": "rs_1", "summary": [], "encrypted_content": "enc", "custom": { "z": 1, "a": 2 } }}`,
			`{"type":"response.completed","response":{"status":"completed","output":[]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc","custom":{"z":1,"a":2}}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})

	t.Run("backfilled", func(t *testing.T) {
		msg := run([]string{
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			added,
			itemDone,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"enc-blob"}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}],"encrypted_content":"enc-blob"}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})

	t.Run("empty-terminal-content-is-ignored", func(t *testing.T) {
		msg := run([]string{
			added,
			itemDone,
			`{"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":""}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})

	t.Run("already-present", func(t *testing.T) {
		msg := run([]string{
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			added,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}],"encrypted_content":"already"}}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"other"}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		if strings.Contains(*block.ThinkingSignature, "other") || !strings.Contains(*block.ThinkingSignature, `"already"`) {
			t.Fatalf("signature = %s", *block.ThinkingSignature)
		}
	})

	t.Run("empty-stored-field-is-backfilled", func(t *testing.T) {
		msg := run([]string{
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			added,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":""}}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"filled"}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"filled"}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})
}
