package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// openai-completions.ts applyAnthropicCacheControl: with cacheControlFormat
// "anthropic" and a retention other than none, the request marks the first
// system/developer message, the last tool, and the last user/assistant/tool
// message, so the provider caches the prefix up to each breakpoint.
func TestBuildOpenAICompletionsParamsAppliesAnthropicCacheControl(t *testing.T) {
	model := testOpenAIModel()
	compat := GetOpenAICompletionsCompat(model)
	compat.CacheControlFormat = "anthropic"
	tools := []Tool{{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)}}
	context := TranscriptContext{
		Messages: []Message{
			&SystemMessage{Content: StringOrBlocks{Text: "instructions"}, ToolsAdded: tools, Timestamp: 0},
			&UserMessage{Content: StringOrBlocks{Text: "first"}, Timestamp: 1},
			&AssistantMessage{Content: ContentList{TextContent{Text: "answer"}}, Timestamp: 2},
			&UserMessage{Content: StringOrBlocks{Text: "second"}, Timestamp: 3},
		},
	}

	params, err := BuildOpenAICompletionsParams(model, context, nil, &compat, CacheRetentionShort, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	// First system message: string content becomes a cached text part.
	if role := params.Messages[0].Role; role != "system" && role != "developer" {
		t.Fatalf("first message role = %q, want the instruction message", role)
	}
	var systemParts []OpenAIContentPart
	if err := json.Unmarshal(params.Messages[0].Content, &systemParts); err != nil {
		t.Fatalf("system content = %s: %v", params.Messages[0].Content, err)
	}
	if len(systemParts) != 1 || systemParts[0].CacheControl == nil || systemParts[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("system parts = %+v, want one cached text part", systemParts)
	}
	// Last tool carries the marker.
	if len(params.Tools) != 1 || params.Tools[0].CacheControl == nil {
		t.Fatalf("tools = %+v, want the last tool marked", params.Tools)
	}
	// Last conversation message carries it.
	last := params.Messages[len(params.Messages)-1]
	var lastParts []OpenAIContentPart
	if err := json.Unmarshal(last.Content, &lastParts); err != nil {
		t.Fatalf("last content = %s: %v", last.Content, err)
	}
	if len(lastParts) != 1 || lastParts[0].CacheControl == nil {
		t.Fatalf("last message parts = %+v, want one cached text part", lastParts)
	}
	// Earlier conversation messages stay unmarked (a plain string content is
	// left alone, which is the marker's absence).
	for _, index := range []int{1, 2} {
		var earlier []OpenAIContentPart
		if err := json.Unmarshal(params.Messages[index].Content, &earlier); err == nil && len(earlier) > 0 && earlier[0].CacheControl != nil {
			t.Fatalf("message %d must not be cached: %s", index, params.Messages[index].Content)
		}
	}

	// Retention none adds no marker at all.
	params, err = BuildOpenAICompletionsParams(model, context, nil, &compat, CacheRetentionNone, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range params.Messages {
		if len(message.Content) > 0 && jsonContainsRaw(message.Content, "cache_control") {
			t.Fatalf("retention none marked %s: %s", message.Role, message.Content)
		}
	}
	if len(params.Tools) == 1 && params.Tools[0].CacheControl != nil {
		t.Fatal("retention none marked the last tool")
	}

	// A compat that is not anthropic adds no marker either.
	other := GetOpenAICompletionsCompat(model)
	other.CacheControlFormat = "openai"
	params, err = BuildOpenAICompletionsParams(model, context, nil, &other, CacheRetentionShort, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range params.Messages {
		if len(message.Content) > 0 && jsonContainsRaw(message.Content, "cache_control") {
			t.Fatalf("openai cache format marked %s: %s", message.Role, message.Content)
		}
	}
}

func jsonContainsRaw(raw json.RawMessage, needle string) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	encoded, err := json.Marshal(value)
	return err == nil && strings.Contains(string(encoded), needle)
}
