package ai

import (
	ctxpkg "context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Tests keyed to packages/ai/test/google-vertex-api-key-resolution.test.ts and
// the Vertex transport contract in api/google-vertex.ts.

func vertexSSEServer(t *testing.T, capture *http.Request, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if capture != nil {
			*capture = *request
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

const vertexStreamBody = `data: {"responseId":"vertex-response-id","candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}

`

func TestVertexRequestURLLayout(t *testing.T) {
	// The generated Vertex base URL is a {location} template.
	model := &Model{
		ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
		BaseURL: "https://{location}-aiplatform.googleapis.com",
	}
	requestURL, err := VertexRequestURL(model, "test-project", "us-central1")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse"
	if requestURL != want {
		t.Fatalf("url = %s", requestURL)
	}

	// A custom base URL is honored and the API version is appended.
	custom := *model
	custom.BaseURL = "https://proxy.example.com"
	requestURL, _ = VertexRequestURL(&custom, "test-project", "us-central1")
	if !strings.HasPrefix(requestURL, "https://proxy.example.com/v1/projects/test-project/") {
		t.Fatalf("url = %s", requestURL)
	}

	// A base URL that already carries an API version is not duplicated.
	withVersion := *model
	withVersion.BaseURL = "https://proxy.example.com/v1/projects/test-project/locations/global"
	requestURL, _ = VertexRequestURL(&withVersion, "test-project", "us-central1")
	if !strings.HasPrefix(requestURL, "https://proxy.example.com/v1/projects/test-project/locations/global/projects/") {
		t.Fatalf("url = %s", requestURL)
	}
	if strings.Contains(requestURL, "/v1/v1/") {
		t.Fatalf("duplicated api version: %s", requestURL)
	}

	// A whitespace-only override behaves like no custom base URL.
	blankOverride := *model
	blankOverride.BaseURL = "\uFEFF"
	requestURL, err = VertexRequestURL(&blankOverride, "p", "us-central1")
	if err != nil || !strings.HasPrefix(requestURL, "https://aiplatform.googleapis.com/v1/projects/p/") {
		t.Fatalf("blank override URL = %q, err = %v; want default Vertex endpoint", requestURL, err)
	}

	// Without a base URL the global endpoint is used.
	plain := *model
	plain.BaseURL = ""
	requestURL, _ = VertexRequestURL(&plain, "p", "us-central1")
	if !strings.HasPrefix(requestURL, "https://aiplatform.googleapis.com/v1/projects/p/") {
		t.Fatalf("url = %s", requestURL)
	}
}

func TestResolveVertexAPIKey(t *testing.T) {
	cases := map[string]string{
		"AIzaSyRealKey":          "AIzaSyRealKey",
		"  AIzaSyRealKey  ":      "AIzaSyRealKey",
		"<authenticated>":        "",
		"gcp-vertex-credentials": "",
		"\uFEFF":                 "",
		"":                       "",
	}
	for input, want := range cases {
		if got := ResolveVertexAPIKey(input); got != want {
			t.Errorf("ResolveVertexAPIKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestVertexStreamWithAPIKey(t *testing.T) {
	var captured http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := json.Marshal(map[string]any{})
		_ = body
		captured = *request
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(vertexStreamBody))
	}))
	defer server.Close()

	model := &Model{
		ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
		BaseURL: server.URL, ContextWindow: 1000, MaxTokens: 100,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}
	stream := StreamGoogleVertex(model, context, &GoogleVertexOptions{
		APIKey:   "AIzaSyRealKey",
		Project:  "test-project",
		Location: "us-central1",
	})
	message, err := stream.Result(ctxpkg.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.API != APIGoogleVertex || message.Model != "gemini-3-flash-preview" {
		t.Fatalf("message = %+v", message)
	}
	if len(message.Content) != 1 {
		t.Fatalf("content = %+v", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != "ok" {
		t.Fatalf("content = %+v", message.Content)
	}
	if captured.Header.Get("x-goog-api-key") != "AIzaSyRealKey" {
		t.Fatalf("api key header = %q", captured.Header.Get("x-goog-api-key"))
	}
	if captured.Header.Get("Authorization") != "" {
		t.Fatalf("unexpected authorization header: %q", captured.Header.Get("Authorization"))
	}
	// Reference: @google/genai 2.21.0 tModel uses Express-mode publisher
	// paths for API-key clients, even when project/location are supplied.
	if captured.URL.Path != "/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent" {
		t.Fatalf("url = %s", captured.URL.String())
	}
}

func TestVertexStreamUsesADCRefreshToken(t *testing.T) {
	// The token endpoint mints a bearer token for an authorized-user file.
	var tokenRequests int
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		tokenRequests++
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("grant_type") != "refresh_token" ||
			request.Form.Get("refresh_token") != "refresh-token" ||
			request.Form.Get("client_id") != "client-id" {
			t.Errorf("token request form = %#v", request.Form)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"adc-token","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	credentials := map[string]any{
		"type": "authorized_user", "client_id": "client-id", "client_secret": "client-secret",
		"refresh_token": "refresh-token", "token_uri": tokenServer.URL,
	}
	encoded, _ := json.Marshal(credentials)
	if err := os.WriteFile(credentialsPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	// The cache is keyed by path; clear it so repeated runs are deterministic.
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()

	var captured http.Request
	modelServer := vertexSSEServer(t, &captured, vertexStreamBody)
	model := &Model{
		ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
		BaseURL: modelServer.URL, ContextWindow: 1000, MaxTokens: 100,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}
	// The ADC marker falls back to credentials instead of sending an API key.
	stream := StreamGoogleVertex(model, context, &GoogleVertexOptions{
		APIKey:   GCPVertexCredentialsMarker,
		Project:  "test-project",
		Location: "us-central1",
	})
	if _, err := stream.Result(ctxpkg.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if captured.Header.Get("Authorization") != "Bearer adc-token" {
		t.Fatalf("authorization = %q", captured.Header.Get("Authorization"))
	}
	if captured.Header.Get("x-goog-api-key") != "" {
		t.Fatalf("unexpected api key header: %q", captured.Header.Get("x-goog-api-key"))
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d", tokenRequests)
	}
}

// Pinned google-vertex.ts constructs a fresh GoogleGenAI per stream; its ADC
// client rereads an authorized-user key file when credentials are rotated.
func TestVertexAuthorizedUserCredentialRotationInvalidatesTokenCache(t *testing.T) {
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()

	requests := make(chan string, 2)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		refreshToken := request.Form.Get("refresh_token")
		requests <- refreshToken
		accessToken := "access-one"
		if refreshToken == "refresh-two" {
			accessToken = "access-two"
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"expires_in":3600,"access_token":"` + accessToken + `"}`))
	}))
	defer tokenServer.Close()

	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	writeCredentials := func(refreshToken string) {
		t.Helper()
		credentials, err := json.Marshal(map[string]string{
			"type": "authorized_user", "client_id": "client-id", "client_secret": "client-secret",
			"refresh_token": refreshToken, "token_uri": tokenServer.URL,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(credentialsPath, credentials, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCredentials("refresh-one")
	env := ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": credentialsPath}
	first, err := vertexAccessToken(ctxpkg.Background(), env)
	if err != nil || first != "access-one" {
		t.Fatalf("first token = %q err = %v", first, err)
	}
	if got := <-requests; got != "refresh-one" {
		t.Fatalf("first refresh token = %q", got)
	}

	writeCredentials("refresh-two")
	second, err := vertexAccessToken(ctxpkg.Background(), env)
	if err != nil || second != "access-two" {
		t.Fatalf("rotated token = %q err = %v; want access-two", second, err)
	}
	if got := <-requests; got != "refresh-two" {
		t.Fatalf("second refresh token = %q; want refresh-two", got)
	}
}

func TestVertexExplicitEmptyAPIKeyDoesNotFallBackToEnv(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_API_KEY", "AIzaSyAmbient")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing-adc.json"))
	t.Setenv("GCE_METADATA_HOST", "127.0.0.1:1")
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()
	model := &Model{ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
		BaseURL: "https://example.invalid", ContextWindow: 1000, MaxTokens: 100}
	context := TranscriptContext{Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1}}}
	stream := StreamGoogleVertex(model, context, &GoogleVertexOptions{
		GoogleOptions: GoogleOptions{APIKey: "", APIKeySet: true}, Project: "test-project", Location: "us-central1",
	})
	message, err := stream.Result(ctxpkg.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(*message.ErrorMessage, "could not read Google credentials") {
		t.Fatalf("message error = %q; want ADC lookup, not GOOGLE_CLOUD_API_KEY fallback", *message.ErrorMessage)
	}
}

func TestVertexStreamRequiresProjectAndLocation(t *testing.T) {
	model := &Model{
		ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
		BaseURL: "https://{location}-aiplatform.googleapis.com",
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GCE_METADATA_HOST", "127.0.0.1:1")

	stream := StreamGoogleVertex(model, context, &GoogleVertexOptions{})
	message, err := stream.Result(ctxpkg.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(*message.ErrorMessage, "Vertex AI requires a project ID") {
		t.Fatalf("message = %+v", message)
	}

	// With a project but no location the location message is reported.
	stream = StreamGoogleVertex(model, context, &GoogleVertexOptions{Project: "test-project"})
	message, err = stream.Result(ctxpkg.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.ErrorMessage == nil || !strings.Contains(*message.ErrorMessage, "Vertex AI requires a location") {
		t.Fatalf("message = %+v", message)
	}
}

func TestVertexStreamServiceAccountCredentialsUnsupported(t *testing.T) {
	credentialsPath := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"type":"service_account","client_email":"a@b.iam.gserviceaccount.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()

	if _, err := vertexAccessToken(ctxpkg.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "service-account key files are not supported") {
		t.Fatalf("err = %v", err)
	}
}

// pi v1.4.0 google-vertex.ts uses GoogleGenAI's ADC flow; its pinned
// google-auth-library computes expiry_date as Date.now() + expires_in * 1000.
func TestVertexMetadataTokenLargeExpiryStaysCached(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"long-lived-token","expires_in":10000000000}`))
	}))
	defer server.Close()

	vertexTokenCache.mu.Lock()
	previous := vertexTokenCache.entries
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()
	t.Cleanup(func() {
		vertexTokenCache.mu.Lock()
		vertexTokenCache.entries = previous
		vertexTokenCache.mu.Unlock()
	})
	env := ProviderEnv{"GCE_METADATA_HOST": strings.TrimPrefix(server.URL, "http://")}
	for i := 0; i < 2; i++ {
		token, err := metadataAccessToken(ctxpkg.Background(), env)
		if err != nil || token != "long-lived-token" {
			t.Fatalf("request %d: token=%q err=%v", i+1, token, err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("metadata requests = %d, want cached token after first request", got)
	}
}

func TestVertexStreamMetadataServerToken(t *testing.T) {
	metadataServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Metadata-Flavor") != "Google" {
			t.Errorf("metadata flavor = %q", request.Header.Get("Metadata-Flavor"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"metadata-token","expires_in":3600}`))
	}))
	defer metadataServer.Close()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(metadataServer.URL, "http://"))
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()

	token, err := vertexAccessToken(ctxpkg.Background(), nil)
	if err != nil || token != "metadata-token" {
		t.Fatalf("token = %q err = %v", token, err)
	}
}

func TestStreamGoogleVertexSimpleThinking(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		captured = body
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(vertexStreamBody))
	}))
	defer server.Close()

	model := &Model{
		ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
		BaseURL: server.URL, Reasoning: true, ContextWindow: 1000, MaxTokens: 100,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}
	// Vertex with a custom base URL still needs project and location unless an
	// API key short-circuits them.
	stream := StreamGoogleVertexSimple(model, context, &SimpleStreamOptions{
		APIKey:    "AIzaSyRealKey",
		Reasoning: ThinkHigh,
	})
	if _, err := stream.Result(ctxpkg.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	// The wire body flattens the SDK's config object into the Gemini API shape.
	config, ok := captured["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("captured = %#v", captured)
	}
	thinking, ok := config["thinkingConfig"].(map[string]any)
	if !ok || thinking["includeThoughts"] != true {
		t.Fatalf("thinkingConfig = %#v", config["thinkingConfig"])
	}
	// Gemini 3 uses the discrete thinking level rather than a token budget.
	if thinking["thinkingLevel"] != "HIGH" {
		t.Fatalf("thinkingLevel = %#v", thinking["thinkingLevel"])
	}
}
