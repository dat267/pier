package coding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// testPNGBase64 renders a solid PNG of the given size.
func testPNGBase64(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 241), B: 40, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// TestSessionNormalizesToolResultImages pins pi v1.5.2 (b4afdddf)
// core/agent-session.ts _toolResultMessage: every tool result's images run
// through normalizeToolResultImages with the session model's resize profile and
// the auto-resize setting before the result enters history, so an oversized
// image cannot reach the provider at full size.
func TestSessionNormalizesToolResultImages(t *testing.T) {
	model := &ai.Model{ID: "vision", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192, Input: []string{"text", "image"}}
	oversized := testPNGBase64(t, 3000, 8)

	var mu sync.Mutex
	var toolResult *ai.ToolResultMessage
	calls := 0
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		mu.Lock()
		calls++
		call := calls
		if call == 2 {
			for _, message := range transcript.Messages {
				if result, ok := message.(*ai.ToolResultMessage); ok {
					copied := *result
					toolResult = &copied
				}
			}
		}
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			if call == 1 {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopToolUse, Message: &ai.AssistantMessage{
					API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
					Content:    ai.ContentList{ai.ToolCall{ID: "shot-1", Name: "shot", Arguments: json.RawMessage(`{}`)}},
					StopReason: ai.StopToolUse,
				}})
				return
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}

	shot := agent.AgentTool{
		Name: "shot", Label: "shot", Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			return agent.AgentToolResult{Content: []ai.Content{
				ai.TextContent{Text: "screenshot"},
				ai.ImageContent{Data: oversized, MimeType: "image/png"},
			}}, nil
		},
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{shot},
		Goal:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	if err := session.PromptText(context.Background(), "take a screenshot"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if toolResult == nil {
		t.Fatal("the second request did not carry a tool result")
	}
	var encoded string
	for _, block := range toolResult.Content {
		if imageBlock, ok := block.(ai.ImageContent); ok {
			encoded = imageBlock.Data
		}
	}
	if encoded == "" {
		t.Fatalf("tool result lost its image: %+v", toolResult.Content)
	}
	if encoded == oversized {
		t.Fatal("the oversized tool-result image reached the provider unchanged")
	}
	data, ok := DecodeImageBase64(encoded)
	if !ok {
		t.Fatal("normalized image is not decodable")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width > DefaultImageResizeOptions.MaxWidth {
		t.Fatalf("normalized width = %d, want at most %d", config.Width, DefaultImageResizeOptions.MaxWidth)
	}
}

// TestSessionKeepsToolResultWithoutImages pins the other half of upstream's
// _toolResultMessage: a result with no image blocks keeps its content and
// details untouched, because normalizeToolResultImages returns the same array.
func TestSessionKeepsToolResultWithoutImages(t *testing.T) {
	model := &ai.Model{ID: "vision", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192, Input: []string{"text", "image"}}
	var mu sync.Mutex
	var toolResult *ai.ToolResultMessage
	calls := 0
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		mu.Lock()
		calls++
		call := calls
		if call == 2 {
			for _, message := range transcript.Messages {
				if result, ok := message.(*ai.ToolResultMessage); ok {
					copied := *result
					toolResult = &copied
				}
			}
		}
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			content := ai.ContentList{ai.TextContent{Text: "done"}}
			reason := ai.StopStop
			if call == 1 {
				content = ai.ContentList{ai.ToolCall{ID: "plain-1", Name: "plain", Arguments: json.RawMessage(`{}`)}}
				reason = ai.StopToolUse
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: reason, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: content, StopReason: reason,
			}})
		}()
		return stream
	}

	plain := agent.AgentTool{
		Name: "plain", Label: "plain", Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: "plain output"}},
				Details: json.RawMessage(`{"kept":true}`),
			}, nil
		},
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{plain},
		Goal:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	if err := session.PromptText(context.Background(), "run"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if toolResult == nil {
		t.Fatal("the second request did not carry a tool result")
	}
	if len(toolResult.Content) != 1 {
		t.Fatalf("content = %+v", toolResult.Content)
	}
	if text, ok := toolResult.Content[0].(ai.TextContent); !ok || text.Text != "plain output" {
		t.Fatalf("content = %+v", toolResult.Content)
	}
	if string(toolResult.Details) != `{"kept":true}` {
		t.Fatalf("details = %s", toolResult.Details)
	}
}
