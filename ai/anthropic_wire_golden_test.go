package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/api/anthropic-messages.ts
// `stream`, whose request body is the JSON.stringify of its buildParams result.
// The golden drives the pinned code with an injected fetch, so each `body` is
// the reference's exact wire bytes: member order, cache_control placement,
// eager tool streaming, the strict-schema arm, the thinking variants and the
// effort markers. Request bodies are a parity surface because a provider sees
// them verbatim and because `onPayload` hooks in both implementations receive
// them.
// Regenerate: node --experimental-strip-types ai/testdata/anthropic-wire/generate.mjs ./pi > ai/testdata/anthropic-wire/golden.json
func TestAnthropicRequestBodiesMatchReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/anthropic-wire/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string `json:"name"`
		Body    string `json:"body"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 9 {
		t.Fatalf("golden holds %d cases", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			model, messages, options := anthropicWireFixture(fixture.Name)
			params, err := BuildAnthropicParams(model, NormalizeContext(Context{Messages: messages}), false, options)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := MarshalJSON(params)
			if err != nil {
				t.Fatal(err)
			}
			// onPayload sees the built params, betas still in the body.
			if string(encoded) != fixture.Payload {
				t.Fatalf("payload =\n%s\nwant\n%s", encoded, fixture.Payload)
			}
			// Sending strips betas into the anthropic-beta header.
			body, _, err := splitAnthropicBetaParams(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != fixture.Body {
				t.Fatalf("body =\n%s\nwant\n%s", body, fixture.Body)
			}
		})
	}
}

// anthropicWireFixture rebuilds the inputs the generator gave the reference, so
// both sides start from the same messages and options.
func anthropicWireFixture(name string) (*Model, []Message, *AnthropicOptions) {
	model := &Model{
		ID: "claude-opus-4-5", Name: "Claude Opus 4.5", API: APIAnthropicMessages, Provider: "anthropic",
		BaseURL: "https://api.anthropic.com", Reasoning: true, Input: []string{"text", "image"},
		Cost:          ModelCost{ModelCostRates: ModelCostRates{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}},
		ContextWindow: 200000, MaxTokens: 64000,
		Compat: &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsStrictTools: new(true)}},
	}
	readTool := Tool{
		Name: "read_file", Description: "Reads a file",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}
	strictTool := Tool{
		Name: "emit", Description: "Emit data",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"number"}},"required":["z","a"],"additionalProperties":false}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}},
	}
	system := func(tools ...Tool) Message {
		return &SystemMessage{Content: StringOrBlocks{Text: "sys"}, ToolsAdded: tools, Timestamp: 0}
	}
	userText := func(text string, timestamp int64) Message {
		return &UserMessage{Content: StringOrBlocks{Text: text}, Timestamp: timestamp}
	}
	assistant := func(content ContentList, stopReason string, timestamp int64) Message {
		return &AssistantMessage{
			API: APIAnthropicMessages, Provider: "anthropic", Model: "claude-opus-4-5",
			StopReason: stopReason, Content: content, Timestamp: timestamp,
		}
	}
	toolResult := func(id, name string, content UserContentList, isError bool, timestamp int64) Message {
		return &ToolResultMessage{ToolCallID: id, ToolName: name, Content: content, IsError: isError, Timestamp: timestamp}
	}

	switch name {
	case "minimal":
		return model, []Message{userText("hi", 1)}, nil
	case "system-tools-cache":
		return model, []Message{
			system(readTool),
			&UserMessage{Content: StringOrBlocks{Blocks: ContentList{
				TextContent{Text: "hi"},
				ImageContent{Data: "aW1hZ2U=", MimeType: "image/png"},
			}}, Timestamp: 1},
		}, nil
	case "thinking-budget":
		return model, []Message{userText("think", 1)}, &AnthropicOptions{
			StreamOptions:        StreamOptions{Temperature: new(0.9), MaxTokens: new(4096)},
			ThinkingEnabled:      new(true),
			ThinkingBudgetTokens: new(2048),
		}
	case "assistant-replay":
		signature, redacted := "c2ln", "cmVk"
		return model, []Message{
			userText("go", 1),
			assistant(ContentList{
				ThinkingContent{Thinking: "plan", ThinkingSignature: &signature},
				ThinkingContent{ThinkingSignature: &redacted, Redacted: true},
				TextContent{Text: "calling"},
				ToolCall{ID: "toolu_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.txt","nested":[1,{"z":null}]}`)},
			}, StopToolUse, 2),
			toolResult("toolu_1", "read_file", UserContentList{TextContent{Text: "missing"}}, true, 3),
			userText("again", 4),
		}, nil
	case "forced-tool":
		return model, []Message{system(readTool), userText("read", 1)}, &AnthropicOptions{
			StreamOptions: StreamOptions{Temperature: new(0.0)},
			ToolChoice:    json.RawMessage(`{"type":"tool","name":"read_file"}`),
		}
	case "strict-schema":
		return model, []Message{system(strictTool), userText("emit", 1)}, nil
	case "adaptive-effort":
		adaptive := *model
		adaptive.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{
			SupportsStrictTools:   new(true),
			ForceAdaptiveThinking: new(true),
		}}
		return &adaptive, []Message{userText("effort", 1)}, &AnthropicOptions{
			ThinkingEnabled: new(true),
			Effort:          AnthropicEffortHigh,
		}
	case "mid-convo-effort":
		midConvo := *model
		midConvo.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{
			SupportsStrictTools:    new(true),
			SupportsMidConvoEffort: new(true),
		}}
		return &midConvo, []Message{
			userText("one", 1),
			assistant(ContentList{TextContent{Text: "first"}}, StopStop, 2),
			userText("two", 3),
		}, &AnthropicOptions{ThinkingEnabled: new(true), Effort: AnthropicEffortMax}
	case "tool-result-image":
		return model, []Message{
			userText("shot", 1),
			assistant(ContentList{ToolCall{ID: "toolu_2", Name: "shoot", Arguments: json.RawMessage(`{}`)}}, StopToolUse, 2),
			toolResult("toolu_2", "shoot", UserContentList{
				TextContent{Text: "here"},
				ImageContent{Data: "aW1n", MimeType: "image/jpeg"},
			}, false, 3),
		}, nil
	}
	panic("unknown anthropic-wire fixture: " + name)
}
