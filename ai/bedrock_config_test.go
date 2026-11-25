package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Tests for api/bedrock-converse-stream.ts (message conversion, request
// construction, and the streaming event fold).

// Pinned bedrock-converse-stream.ts passes the selected profile to the SDK;
// the SDK resolves its region from that profile's shared config file.
func TestBedrockProfileConfigRegionSignsRequestsInThatRegion(t *testing.T) {
	for _, name := range []string{"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	credentialsPath := filepath.Join(dir, "credentials")
	if err := os.WriteFile(configPath, []byte("[profile work]\nregion = us-west-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, []byte("[work]\naws_access_key_id = AKID\naws_secret_access_key = SECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, profileSource := range []string{"option", "environment"} {
		t.Run(profileSource, func(t *testing.T) {
			frames := [][]byte{bedrockEventFrame("messageStart", map[string]any{"role": "assistant"})}
			var captured http.Request
			server := bedrockTestServer(t, frames, &captured, nil)
			env := ProviderEnv{
				"AWS_CONFIG_FILE": configPath, "AWS_SHARED_CREDENTIALS_FILE": credentialsPath,
			}
			options := &BedrockOptions{Env: env}
			if profileSource == "option" {
				options.Profile = "work"
			} else {
				env["AWS_PROFILE"] = "work"
			}
			stream := StreamBedrockConverse(bedrockTestModel(server.URL), TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
			}}, options)
			_, _ = stream.Result(context.Background())
			authorization := captured.Header.Get("Authorization")
			if !strings.Contains(authorization, "/us-west-2/bedrock/aws4_request") {
				t.Fatalf("authorization = %q; want profile region us-west-2", authorization)
			}
		})
	}
}

func TestBedrockProviderFactory(t *testing.T) {
	provider := AmazonBedrockProvider()
	if provider.ID != "amazon-bedrock" || provider.Name != "Amazon Bedrock" {
		t.Fatalf("provider = %+v", provider)
	}
	if provider.Auth.APIKey == nil || provider.Auth.APIKey.Name != "AWS credentials or bearer token" {
		t.Fatalf("auth = %+v", provider.Auth)
	}
	if len(provider.GetModels()) == 0 {
		t.Fatal("no catalog models")
	}
	// The auth resolver detects ambient credentials.
	result, err := provider.Auth.APIKey.Resolve(AuthResolveInput{
		Ctx: staticAuthContext{"AWS_ACCESS_KEY_ID": "AKID", "AWS_SECRET_ACCESS_KEY": "SECRET"},
	})
	if err != nil || result == nil || result.Source != "AWS access keys" {
		t.Fatalf("result = %+v err = %v", result, err)
	}
	result, err = provider.Auth.APIKey.Resolve(AuthResolveInput{Ctx: staticAuthContext{}})
	if err != nil || result != nil {
		t.Fatalf("result = %+v err = %v", result, err)
	}
}

// staticAuthContext is an injected auth context.
type staticAuthContext map[string]string

// pi v1.5.0 bedrock-converse-stream.ts retries a preferred strict tool once
// without strict mode when Bedrock rejects it for strict-tool limits, and
// records the fallback diagnostic.
func TestBedrockStrictToolLimitFallsBack(t *testing.T) {
	frames := [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"text": "ok"}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 0}),
		bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
	}
	var bodies []map[string]any
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		if requests == 1 {
			writer.Header().Set("x-amzn-errortype", "ValidationException")
			writer.Header().Set("x-amzn-requestid", "req-1")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"message":"Too many strict tools (20)"}`))
			return
		}
		writer.Header().Set("x-amzn-requestid", "req-2")
		writer.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writer.WriteHeader(http.StatusOK)
		for _, frame := range frames {
			_, _ = writer.Write(frame)
		}
	}))
	defer server.Close()

	model := bedrockTestModel(server.URL)
	strictSupport := true
	model.Compat = &ModelCompat{Bedrock: &BedrockCompat{SupportsStrictMode: &strictSupport}}
	tool := Tool{
		Name: "strict", Description: "strict tool",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"],"additionalProperties":false}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}},
	}
	stream := StreamBedrockConverse(model, TranscriptContext{Messages: []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "be helpful"}, ToolsAdded: []Tool{tool}},
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{APIKey: "bearer-token", BearerToken: "bearer-token"})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop || requests != 2 {
		t.Fatalf("stop=%s requests=%d", message.StopReason, requests)
	}
	first := bodies[0]["toolConfig"].(map[string]any)["tools"].([]any)[0].(map[string]any)["toolSpec"].(map[string]any)
	second := bodies[1]["toolConfig"].(map[string]any)["tools"].([]any)[0].(map[string]any)["toolSpec"].(map[string]any)
	if first["strict"] != true {
		t.Fatalf("first toolSpec strict = %v", first["strict"])
	}
	if _, ok := second["strict"]; ok {
		t.Fatalf("retry kept strict mode: %v", second)
	}
	if len(message.Diagnostics) != 1 || message.Diagnostics[0].Type != "bedrock_strict_tools_fallback" {
		t.Fatalf("diagnostics = %+v", message.Diagnostics)
	}
}

// Upstream pi v1.5.0 (f9a26365) bedrock-converse-stream.ts, from f76c1db66
// "feat(ai): add Claude Haiku 5.5": Haiku 5 is detected for adaptive thinking,
// native xhigh, thinking block binding and prompt caching, so a Bedrock Haiku
// 5.5 request sends adaptive effort and cache points instead of a token budget.
func TestBedrockHaiku5IsAClaude5Model(t *testing.T) {
	for _, id := range []string{"anthropic.claude-haiku-5-5", "us.anthropic.claude-haiku-5-5", "eu.anthropic.claude-haiku-5-5"} {
		model := &Model{ID: id, Name: "Claude Haiku 5.5", Reasoning: true}
		if !SupportsAdaptiveThinking(model.ID, model.Name) {
			t.Errorf("%s must support adaptive thinking", id)
		}
		if !SupportsNativeXhighEffort(model) {
			t.Errorf("%s must support native xhigh", id)
		}
		if !SupportsThinkingBlockBinding(model) {
			t.Errorf("%s must support thinking block binding", id)
		}
		if !SupportsBedrockPromptCaching(model, nil) {
			t.Errorf("%s must support prompt caching", id)
		}
		fields := BuildBedrockAdditionalModelRequestFields(model, &BedrockOptions{Reasoning: ThinkXHigh})
		raw, err := MarshalJSON(fields)
		if err != nil {
			t.Fatal(err)
		}
		// Node: buildAdditionalModelRequestFields for the same model and options.
		want := `{"thinking":{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"}},"output_config":{"effort":"xhigh"},"anthropic_beta":["thinking-binding-controls-2026-08-01"]}`
		var got, expected any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(want), &expected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("%s fields = %s\nwant %s", id, raw, want)
		}
	}
}

// Upstream sets cacheWrite1h from `usage.cacheDetails?.reduce(...)`
// (packages/ai/src/api/bedrock-converse-stream.ts handleMetadata), so the member
// stays undefined when the metadata carries no cacheDetails, and
// JSON.stringify drops it. The port always wrote the pointer, persisting
// "cacheWrite1h":0 for every model that reports no cache details.
func TestBedrockMetadataWithoutCacheDetailsOmitsCacheWrite1h(t *testing.T) {
	frames := [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"text": "hi"}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 0}),
		bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
		bedrockEventFrame("metadata", map[string]any{"usage": map[string]any{
			"inputTokens": 10, "outputTokens": 5, "totalTokens": 15,
		}}),
	}
	var captured http.Request
	server := bedrockTestServer(t, frames, &captured, nil)
	model := bedrockTestModel(server.URL)
	stream := StreamBedrockConverse(model, TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{Env: ProviderEnv{
		"AWS_ACCESS_KEY_ID": "AKID", "AWS_SECRET_ACCESS_KEY": "SECRET",
	}})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.Usage.CacheWrite1h != nil {
		t.Fatalf("cacheWrite1h = %v, want the member absent", *message.Usage.CacheWrite1h)
	}
}

// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/bedrock-converse-stream.ts
// `id.replace(/[\s_.:]+/g, "-")`: JavaScript's \s includes the Unicode space
// separators, so model-match candidates normalize them to "-" as well.
func TestBedrockCandidateSeparatorsUseJavaScriptWhitespace(t *testing.T) {
	for _, separator := range []struct {
		name  string
		value string
	}{
		{"ASCII space", " "},
		{"tab", "\t"},
		{"no-break space", "\u00a0"},
		{"ideographic space", "\u3000"},
		{"underscore", "_"},
		{"dot", "."},
		{"colon", ":"},
	} {
		if got, want := bedrockNormalizeCandidate("a"+separator.value+"b"), "a-b"; got != want {
			t.Errorf("%s: normalized %q, want %q", separator.name, got, want)
		}
	}
	// A non-separator keeps its character.
	if got := bedrockNormalizeCandidate("a-b"); got != "a-b" {
		t.Errorf("normalized %q, want %q", got, "a-b")
	}
}

// D518: model matching lowercases with JS toLowerCase, whose full mapping differs
// from strings.ToLower on U+0130 and the Greek final sigma. The reference's
// getModelMatchCandidates calls value.toLowerCase(), so the candidate must be
// "i̇" (i + combining dot) for İ and "ας" for ΑΣ, not "i" and "ασ".
func TestGetModelMatchCandidatesLowersLikeJavaScript(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"İ", "i̇"},
		{"ΑΣ", "ας"},
		{"CLAUDE-OPUS-4-6", "claude-opus-4-6"},
	} {
		candidates := GetModelMatchCandidates(tc.id, "")
		if len(candidates) == 0 || candidates[0] != tc.want {
			t.Fatalf("candidates(%q) = %q, want first %q", tc.id, candidates, tc.want)
		}
	}
}

// The same JS mapping applies to the Claude-model check, which the reference
// writes as model.id.toLowerCase().
func TestIsAnthropicClaudeModelLowersLikeJavaScript(t *testing.T) {
	model := &Model{ID: "İ", Name: "ΑΣ anthropic.claude"}
	if !IsAnthropicClaudeModel(model) {
		t.Fatal("the name's anthropic.claude must still match")
	}
	// U+0130 in the id must not become a bare "i" that hides the dotless form.
	if IsAnthropicClaudeModel(&Model{ID: "anthropic/claude", Name: ""}) != true {
		t.Fatal("anthropic/claude must match")
	}
}
