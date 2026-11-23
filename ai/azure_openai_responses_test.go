package ai

import (
	ctxpkg "context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Tests ported from packages/ai/test/azure-openai-base-url.test.ts and
// azure-openai-tool-choice.test.ts (the client-construction assertions become
// request-path assertions in Go).

func TestNormalizeAzureBaseURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://marc-quicktests-resource.cognitiveservices.azure.com", "https://marc-quicktests-resource.cognitiveservices.azure.com/openai/v1"},
		{"https://marc-quicktests-resource.ai.azure.com", "https://marc-quicktests-resource.ai.azure.com/openai/v1"},
		{"https://my-resource.openai.azure.com", "https://my-resource.openai.azure.com/openai/v1"},
		{"https://my-resource.cognitiveservices.azure.com/openai", "https://my-resource.cognitiveservices.azure.com/openai/v1"},
		{"https://my-resource.cognitiveservices.azure.com/openai/v1", "https://my-resource.cognitiveservices.azure.com/openai/v1"},
		{"https://my-resource.services.ai.azure.com/openai/v1/responses", "https://my-resource.services.ai.azure.com/openai/v1"},
		{"https://my-proxy.example.com/v1", "https://my-proxy.example.com/v1"},
		// A port with leading zeros is the same port: the WHATWG URL parser
		// reads it as a number, so :0443 on https is the default 443 and is
		// dropped, while :0080 on https is port 80 and is kept.
		{"https://my-proxy.example.com:0443/v1", "https://my-proxy.example.com/v1"},
		{"https://my-resource.openai.azure.com:0443", "https://my-resource.openai.azure.com/openai/v1"},
		{"https://my-proxy.example.com:0080/v1", "https://my-proxy.example.com:80/v1"},
		{"HTTPS://MY-PROXY.EXAMPLE.COM:443/v1", "https://my-proxy.example.com/v1"},
		{"HTTP://MY-PROXY.EXAMPLE.COM:80/v1", "http://my-proxy.example.com/v1"},
		{"https://my-resource.openai.azure.com/openai?api-version=2024-12-01", "https://my-resource.openai.azure.com/openai/v1"},
		{"https://my-resource.openai.azure.com/%6fpenai", "https://my-resource.openai.azure.com/%6fpenai"},
		// Query parameters on non-Azure hosts are preserved.
		{"https://my-proxy.example.com/v1?custom=true", "https://my-proxy.example.com/v1?custom=true"},
		// A WHATWG URL always serializes a path, so an empty path with a query is
		// `host/?q`, not `host?q`.
		{"https://my-proxy.example.com?x=1", "https://my-proxy.example.com/?x=1"},
		// The WHATWG parser resolves dot segments while parsing.
		{"https://my-proxy.example.com/a/../v1", "https://my-proxy.example.com/v1"},
		{"https://my-proxy.example.com/./v1", "https://my-proxy.example.com/v1"},
		{"https://my-proxy.example.com/v1/..", "https://my-proxy.example.com"},
		{"https://my-proxy.example.com/v1/./x", "https://my-proxy.example.com/v1/x"},
		{"\uFEFFhttps://my-proxy.example.com/v1\uFEFF", "https://my-proxy.example.com/v1"},
	}
	for _, testCase := range cases {
		got, err := NormalizeAzureBaseURL(testCase.input)
		if err != nil {
			t.Errorf("NormalizeAzureBaseURL(%q) errored: %v", testCase.input, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("NormalizeAzureBaseURL(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
	if _, err := NormalizeAzureBaseURL("not-a-url"); err == nil {
		t.Fatal("invalid URLs must be rejected")
	}
	// The WHATWG parser rejects a port above 65535.
	for _, outOfRange := range []string{"https://my-proxy.example.com:65536/v1", "https://my-proxy.example.com:99999999999999999999/v1"} {
		if _, err := NormalizeAzureBaseURL(outOfRange); err == nil {
			t.Errorf("NormalizeAzureBaseURL(%q) must be rejected: the WHATWG parser refuses a port above 65535", outOfRange)
		}
	}
	// D511: the reference's WHATWG URL accepts any scheme it knows, so
	// `ftp://host/` parses there; the port requires http(s) because a base URL
	// dialed over ftp cannot serve a Responses request.
	if _, err := NormalizeAzureBaseURL("ftp://my-resource.openai.azure.com"); err == nil {
		t.Fatal("D511: a non-http(s) scheme must be rejected")
	}
}

func TestResolveAzureConfig(t *testing.T) {
	model := &Model{ID: "gpt-4o-mini", API: APIAzureOpenAIResponses, Provider: "azure-openai-responses"}

	// AZURE_OPENAI_RESOURCE_NAME builds the default base URL.
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "my-resource")
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	baseURL, apiVersion, err := ResolveAzureConfig(model, nil)
	if err != nil || baseURL != "https://my-resource.openai.azure.com/openai/v1" || apiVersion != DefaultAzureAPIVersion {
		t.Fatalf("base = %q version = %q err = %v", baseURL, apiVersion, err)
	}

	// The environment API version wins over the default.
	t.Setenv("AZURE_OPENAI_API_VERSION", "2024-12-01")
	if _, apiVersion, _ := ResolveAzureConfig(model, nil); apiVersion != "2024-12-01" {
		t.Fatalf("apiVersion = %q", apiVersion)
	}

	// Options win over the environment, and the model base URL is the fallback.
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	withBase := *model
	withBase.BaseURL = "https://proxy.example.com/v1"
	baseURL, _, err = ResolveAzureConfig(&withBase, nil)
	if err != nil || baseURL != "https://proxy.example.com/v1" {
		t.Fatalf("base = %q err = %v", baseURL, err)
	}
	baseURL, _, err = ResolveAzureConfig(model, &AzureOpenAIResponsesOptions{
		AzureBaseURL: "https://opt.openai.azure.com", AzureAPIVersion: "v9",
	})
	if err != nil || baseURL != "https://opt.openai.azure.com/openai/v1" {
		t.Fatalf("base = %q err = %v", baseURL, err)
	}
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "my-resource")
	baseURL, _, err = ResolveAzureConfig(model, &AzureOpenAIResponsesOptions{AzureBaseURL: "\uFEFF"})
	if err != nil || baseURL != "https://my-resource.openai.azure.com/openai/v1" {
		t.Fatalf("BOM-only override base = %q err = %v; want resource fallback", baseURL, err)
	}

	// Without any configuration the upstream error is reported.
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	if _, _, err := ResolveAzureConfig(model, nil); err == nil ||
		!strings.Contains(err.Error(), "Azure OpenAI base URL is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveAzureDeploymentName(t *testing.T) {
	model := &Model{ID: "gpt-4o-mini"}
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-4o-mini=my-deployment,other=second")
	if got := ResolveAzureDeploymentName(model, nil); got != "my-deployment" {
		t.Fatalf("deployment = %q", got)
	}
	// An unmapped model uses its own id; options win.
	if got := ResolveAzureDeploymentName(&Model{ID: "unmapped"}, nil); got != "unmapped" {
		t.Fatalf("deployment = %q", got)
	}
	if got := ResolveAzureDeploymentName(model, &AzureOpenAIResponsesOptions{AzureDeploymentName: "explicit"}); got != "explicit" {
		t.Fatalf("deployment = %q", got)
	}
	// Malformed entries are skipped.
	mapValue := ParseDeploymentNameMap("a=1,,=2,b=,c=3")
	if len(mapValue) != 2 || mapValue["a"] != "1" || mapValue["c"] != "3" {
		t.Fatalf("map = %#v", mapValue)
	}
}

func TestAzureResponsesRequest(t *testing.T) {
	var captured *http.Request
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = request
		raw, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(raw, &body)
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
	}))
	defer server.Close()

	model := &Model{
		ID: "gpt-4o-mini", API: APIAzureOpenAIResponses, Provider: "azure-openai-responses",
		BaseURL: server.URL + "/openai/v1", ContextWindow: 128000, MaxTokens: 4096,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")

	stream := StreamAzureOpenAIResponses(model, context, &AzureOpenAIResponsesOptions{
		APIKey: "test-api-key", SessionID: strings.Repeat("x", 67),
	})
	message, err := stream.Result(ctxpkg.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("message = %+v", message)
	}

	if captured == nil {
		t.Fatal("no request captured")
	}
	// Azure authenticates with the api-key header, not bearer.
	if captured.Header.Get("api-key") != "test-api-key" {
		t.Fatalf("api-key = %q", captured.Header.Get("api-key"))
	}
	if captured.Header.Get("Authorization") != "" {
		t.Fatalf("unexpected authorization: %q", captured.Header.Get("Authorization"))
	}
	// The base URL is normalized and the api-version query is added.
	if !strings.HasSuffix(captured.URL.Path, "/responses") {
		t.Fatalf("path = %s", captured.URL.Path)
	}
	if captured.URL.Query().Get("api-version") != DefaultAzureAPIVersion {
		t.Fatalf("api-version = %q", captured.URL.Query().Get("api-version"))
	}
	// The request is not rewritten to /deployments for responses.
	if strings.Contains(captured.URL.Path, "/deployments/") {
		t.Fatalf("path = %s", captured.URL.Path)
	}
	if captured.Header.Get("User-Agent") == "" {
		t.Fatal("user agent missing")
	}

	// The body keeps the OpenAI Responses shape with Azure specifics.
	if body["store"] != false {
		t.Fatalf("store = %#v", body["store"])
	}
	if body["stream"] != true {
		t.Fatalf("stream = %#v", body["stream"])
	}
	// prompt_cache_key is clamped to 64 characters.
	if key, _ := body["prompt_cache_key"].(string); len(key) != 64 {
		t.Fatalf("prompt_cache_key = %q (%d)", key, len(key))
	}
	if body["model"] != "gpt-4o-mini" {
		t.Fatalf("model = %#v", body["model"])
	}
}

func TestAzureResponsesDeploymentNameInBody(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(raw, &body)
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n"))
	}))
	defer server.Close()

	model := &Model{
		ID: "gpt-4o-mini", API: APIAzureOpenAIResponses, Provider: "azure-openai-responses",
		BaseURL: server.URL, ContextWindow: 128000, MaxTokens: 4096,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-4o-mini=deployment-name")

	stream := StreamAzureOpenAIResponses(model, context, &AzureOpenAIResponsesOptions{
		APIKey: "key",
	})
	if _, err := stream.Result(ctxpkg.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if body["model"] != "deployment-name" {
		t.Fatalf("model = %#v", body["model"])
	}
}

func TestAzureResponsesReasoningEffort(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(raw, &body)
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n"))
	}))
	defer server.Close()

	model := &Model{
		ID: "o4-mini", API: APIAzureOpenAIResponses, Provider: "azure-openai-responses",
		BaseURL: server.URL, Reasoning: true, ContextWindow: 200000, MaxTokens: 100000,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")

	stream := StreamAzureOpenAIResponsesSimple(model, context, &SimpleStreamOptions{
		APIKey:    "key",
		Reasoning: ThinkHigh,
	})
	if _, err := stream.Result(ctxpkg.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning = %#v", body["reasoning"])
	}
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	include, _ := body["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", body["include"])
	}
}

// Upstream's Azure client sets only identity, model and caller headers plus the
// SDK's api-key (packages/ai/src/api/azure-openai-responses.ts createClient);
// the Azure dialect has no session-affinity concept, so it sends none of the
// OpenAI affinity headers even when a session id reaches the stream. The port
// reused the OpenAI responses builder's affinity block, which put session_id,
// x-client-request-id and x-session-id on every Azure request.
func TestAzureResponsesOmitsSessionAffinityHeaders(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = request
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	model := &Model{
		ID: "gpt-4o-mini", API: APIAzureOpenAIResponses, Provider: "azure-openai-responses",
		BaseURL: server.URL + "/openai/v1", ContextWindow: 128000, MaxTokens: 4096,
	}
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	stream := StreamAzureOpenAIResponses(model, TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}, &AzureOpenAIResponsesOptions{APIKey: "test-api-key", SessionID: "sess-1"})
	if _, err := stream.Result(ctxpkg.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if captured == nil {
		t.Fatal("no request captured")
	}
	for _, name := range []string{"session_id", "session-id", "x-client-request-id", "x-session-affinity", "x-session-id"} {
		if value := captured.Header.Get(name); value != "" {
			t.Fatalf("%s = %q, want no session affinity header on Azure", name, value)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) api/azure-openai-responses.ts declares
// `AzureOpenAIResponsesOptions` without a `serviceTier` field and never reads
// one, so the azure request body never carries `service_tier`. The port embeds
// the shared OpenAIResponsesOptions in AzureOpenAIResponsesOptions, so a caller
// that set ServiceTier made the shared builder send a field the reference does
// not (A453).
func TestAzureResponsesOmitsServiceTier(t *testing.T) {
	temp := 0.3
	options := func() *OpenAIResponsesOptions {
		return &OpenAIResponsesOptions{
			MaxTokens: new(1000), Temperature: &temp,
			ServiceTier: "flex",
		}
	}
	context := NormalizeContext(Context{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}})
	body := func(t *testing.T, api Api) string {
		t.Helper()
		model := &Model{ID: "gpt-4.1", Name: "gpt-4.1", API: api, Provider: "azure",
			BaseURL: "https://invalid.test/v1", Input: []string{"text"},
			ContextWindow: 128000, MaxTokens: 8192}
		params, err := BuildOpenAIResponsesParams(model, context, options(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := MarshalJSON(params)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}

	azure := body(t, APIAzureOpenAIResponses)
	if strings.Contains(azure, "service_tier") {
		t.Errorf("azure body carries service_tier: %s", azure)
	}
	// Control: the openai-responses adapter does forward the tier.
	openai := body(t, APIOpenAIResponses)
	if !strings.Contains(openai, `"service_tier":"flex"`) {
		t.Errorf("openai body lost service_tier: %s", openai)
	}
	// The rest of the body is unchanged: only the tier field goes away.
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(azure), &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded["max_output_tokens"]) != "1000" {
		t.Errorf("azure max_output_tokens = %s, want 1000; body %s", decoded["max_output_tokens"], azure)
	}
}
