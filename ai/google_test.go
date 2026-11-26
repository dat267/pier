package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/internal/boundedio"
)

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/api/google-shared.ts
// convertMessages builds `const contents: Content[] = []`, so an empty
// transcript yields an empty contents array. Pier's nil slice marshals as
// `null` instead, and google_payload writes that to the wire, so an empty
// context would send `"contents": null` where the reference sends `[]`.
func TestConvertGoogleMessagesEmptyIsAnArray(t *testing.T) {
	got, err := ConvertGoogleMessages(&Model{ID: "gemini-2.5-pro", Provider: "google"}, TranscriptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("contents is nil, want an empty non-nil slice")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("empty contents = %s, want []", encoded)
	}
	if strings.Contains(string(encoded), "null") {
		t.Fatalf("contents contains null: %s", encoded)
	}
}

// Reference: pi v1.4.0 (4a080252), api/google-generative-ai.ts catch with the
// @google/genai throwErrorIfNotOK message composition. A JSON body is
// JSON.parse/JSON.stringify'd; any other content type is wrapped in the SDK's
// error envelope. D269 records the unparseable-JSON wording difference.
func TestGoogleAPIErrorMessage(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		statusLine  string
		contentType string
		body        string
		want        string
	}{
		{
			name: "json body is re-stringified", status: 401, statusLine: "401 Unauthorized",
			contentType: "application/json",
			body:        `{"error":{"code":401,"message":"API key not valid"}}`,
			want:        `{"error":{"code":401,"message":"API key not valid"}}`,
		},
		{
			name: "json member order and compacting", status: 403, statusLine: "403 Forbidden",
			contentType: "application/json; charset=utf-8",
			body:        "{\n  \"z\": 1,\n  \"a\": {\"y\": 2.0, \"b\": 3}\n}",
			want:        `{"z":1,"a":{"y":2,"b":3}}`,
		},
		{
			name: "text body is wrapped", status: 403, statusLine: "403 Forbidden",
			contentType: "text/plain", body: "nope",
			want: `{"error":{"message":"nope","code":403,"status":"Forbidden"}}`,
		},
		{
			name: "html body is escaped like JavaScript", status: 502, statusLine: "502 Bad Gateway",
			contentType: "text/html", body: "<html>bad gateway</html>",
			want: `{"error":{"message":"<html>bad gateway</html>","code":502,"status":"Bad Gateway"}}`,
		},
		{
			// D269: upstream surfaces V8's JSON.parse SyntaxError here.
			name: "unparseable json keeps decoder wording", status: 403, statusLine: "403 Forbidden",
			contentType: "application/json", body: "nope",
			want: "invalid character 'o' in literal null (expecting 'u')",
		},
		{
			// D269: upstream reports "Unexpected end of JSON input".
			name: "empty json keeps decoder wording", status: 400, statusLine: "400 Bad Request",
			contentType: "application/json", body: "",
			want: "unexpected end of JSON input",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := googleAPIErrorMessage(tc.status, tc.statusLine, tc.contentType, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("message = %q\nwant      %q", got, tc.want)
			}
		})
	}
}

// pi v1.5.0 google-shared.ts rewriteConstAsEnum: Gemini ignores `const` in
// parametersJsonSchema, so string and number constants become single-value
// enums; a property named "const" and a schema that already has an enum stay.
func TestRewriteGoogleConstAsEnum(t *testing.T) {
	cases := map[string]struct {
		schema string
		want   string
	}{
		"string const": {
			`{"type":"object","properties":{"mode":{"type":"string","const":"fast"}},"required":["mode"]}`,
			`{"type":"object","properties":{"mode":{"type":"string","enum":["fast"]}},"required":["mode"]}`,
		},
		"number const": {
			`{"type":"object","properties":{"n":{"const":3,"type":"number"}}}`,
			`{"type":"object","properties":{"n":{"type":"number","enum":[3]}}}`,
		},
		"property named const": {
			`{"type":"object","properties":{"const":{"type":"string","const":"x"}}}`,
			`{"type":"object","properties":{"const":{"type":"string","enum":["x"]}}}`,
		},
		"enum already present": {
			`{"type":"object","properties":{"m":{"const":"a","enum":["a","b"]}}}`,
			`{"type":"object","properties":{"m":{"const":"a","enum":["a","b"]}}}`,
		},
		"boolean const unchanged": {
			`{"type":"object","properties":{"b":{"const":true}}}`,
			`{"type":"object","properties":{"b":{"const":true}}}`,
		},
		"array of subschemas": {
			`{"allOf":[{"const":"x"}]}`,
			`{"allOf":[{"enum":["x"]}]}`,
		},
	}
	for name, probe := range cases {
		if got := string(rewriteGoogleConstAsEnum(json.RawMessage(probe.schema), false)); got != probe.want {
			t.Fatalf("%s = %s, want %s", name, got, probe.want)
		}
	}
}

// The Gemini function declaration sends the rewritten schema.
func TestConvertGoogleToolsRewritesConstToEnum(t *testing.T) {
	tools := []Tool{{
		Name: "probe", Description: "d",
		Parameters: json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","const":"fast"}}}`),
	}}
	groups, err := ConvertGoogleTools(tools, false, false)
	if err != nil {
		t.Fatal(err)
	}
	schema := string(groups[0].FunctionDeclarations[0].ParametersJSONSchema)
	if !strings.Contains(schema, `"enum":["fast"]`) || strings.Contains(schema, `"const"`) {
		t.Fatalf("parametersJsonSchema = %s", schema)
	}
}

// The reference sanitizer walks the parsed schema object with Object.entries,
// so member order survives, and it returns an array as it stands
// (google-shared.ts sanitizeForOpenApi).
func TestGoogleLegacyParametersKeepSchemaKeyOrder(t *testing.T) {
	convert := func(parameters string) string {
		t.Helper()
		groups, err := ConvertGoogleTools([]Tool{{
			Name: "t", Description: "d", Parameters: json.RawMessage(parameters),
		}}, true, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 || len(groups[0].FunctionDeclarations) != 1 {
			t.Fatalf("groups = %#v", groups)
		}
		return string(groups[0].FunctionDeclarations[0].Parameters)
	}
	// Declared order, not sorted order.
	schema := `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`
	if got := convert(schema); got != schema {
		t.Fatalf("parameters = %s, want the schema's own order %s", got, schema)
	}
	// Meta declarations go, at any depth of an object.
	schema = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"a":{"$id":"x","type":"string"}}}`
	want := `{"type":"object","properties":{"a":{"type":"string"}}}`
	if got := convert(schema); got != want {
		t.Fatalf("parameters = %s, want %s", got, want)
	}
	// An array is returned as it stands, as the reference does.
	schema = `{"type":"object","anyOf":[{"$schema":"x","type":"string"}]}`
	if got := convert(schema); got != schema {
		t.Fatalf("parameters = %s, want the array untouched: %s", got, schema)
	}
}

// RetainThoughtSignature keeps the last non-empty signature for the current
// streamed block (backends may send it only on the first delta, and a later
// delta can carry an empty string). An empty value must not clear it.
func TestRetainThoughtSignature(t *testing.T) {
	first := "sig-one"
	second := "sig-two"
	empty := ""
	for _, testCase := range []struct {
		name     string
		existing string
		incoming *string
		want     string
	}{
		{"absent keeps", "sig-one", nil, "sig-one"},
		{"empty keeps", "sig-one", &empty, "sig-one"},
		{"value replaces", "sig-one", &second, "sig-two"},
		{"empty with nothing keeps", "", &empty, ""},
		{"value from nothing", "", &first, "sig-one"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := RetainThoughtSignature(testCase.existing, testCase.incoming); got != testCase.want {
				t.Fatalf("RetainThoughtSignature(%q, %v) = %q, want %q", testCase.existing, testCase.incoming, got, testCase.want)
			}
		})
	}
}

// pi v1.4.0 google-generative-ai.ts consumes the SDK's event stream; its SSE
// reader accepts CR, LF, and CRLF line boundaries.
func TestGoogleChunkParserAcceptsCROnlyEventBoundaries(t *testing.T) {
	first := `{"candidates":[{"content":{"parts":[{"text":"one"}]}}]}`
	second := `{"candidates":[{"content":{"parts":[{"text":"two"}]}}]}`
	body := "data: " + first + "\r\rdata: " + second + "\r\r"
	var texts []string
	err := iterateGoogleChunks(context.Background(), strings.NewReader(body), func(chunk *googleChunk) error {
		if len(chunk.Candidates) != 0 && chunk.Candidates[0].Content != nil && len(chunk.Candidates[0].Content.Parts) != 0 {
			texts = append(texts, chunk.Candidates[0].Content.Parts[0].Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("iterateGoogleChunks returned error: %v", err)
	}
	if strings.Join(texts, ",") != "one,two" {
		t.Fatalf("parsed event texts = %q, want one,two", strings.Join(texts, ","))
	}
}

// pi v1.4.0 api/google-generative-ai.ts keys block creation on
// `part.text !== undefined`, so an empty text part still opens a block and
// emits a delta.
func TestGoogleStreamKeepsPresentButEmptyTextParts(t *testing.T) {
	events := []string{
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]}}]}\n\n",
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"call_1\",\"name\":\"lookup\",\"args\":{}}}]},\"finishReason\":\"STOP\"}]}\n\n",
	}
	server := sseServer(t, events, nil)
	defer server.Close()

	model := testGoogleModel()
	model.BaseURL = server.URL + "/v1beta"
	stream := StreamGoogleGenerativeAI(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &GoogleOptions{APIKey: "test-key"})

	var types []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
	}
	if got, want := joinTypes(types), "start,text_start,text_delta,text_end,toolcall_start,toolcall_delta,toolcall_end,done"; got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 {
		t.Fatalf("content = %+v", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != "" {
		t.Fatalf("first block = %+v", message.Content[0])
	}
}

func joinTypes(types []string) string {
	out := ""
	for i, value := range types {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}

// pi v1.4.0 api/google-generative-ai.ts and google-shared.ts stream
// transitions, executed by the pinned module. Regenerate:
//
//	node --experimental-strip-types ai/testdata/google-stream/generate.mjs ./pi \
//	  > ai/testdata/google-stream/golden.json
//
// Only the event's own scalars and the terminal message are compared; see
// anthropic_stream_events_parity_test.go for why `partial` is excluded.
func TestGoogleStreamEventsMatchUpstream(t *testing.T) {
	runStreamEventsCorpus(t, "testdata/google-stream/golden.json", "google stream", func(baseURL string) *AssistantMessageEventStream {
		model := testGoogleModel()
		model.ID = "gemini-3-pro"
		model.Name = "Gemini 3 Pro"
		model.BaseURL = baseURL + "/v1beta"
		model.Cost = ModelCost{Input: 2, Output: 12, CacheRead: 0.5}

		return StreamGoogleGenerativeAI(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &GoogleOptions{APIKey: "test-key"})
	})
}

// vertexADCTokenAccess resolves an ADC access token from an authorized-user
// credential file whose token endpoint is tokenURL.
func vertexADCTokenAccess(t *testing.T, tokenURL string) (string, error) {
	t.Helper()
	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	encoded, err := json.Marshal(map[string]any{
		"type": "authorized_user", "client_id": "client-id", "client_secret": "client-secret",
		"refresh_token": "refresh-token", "token_uri": tokenURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(credentialsPath, encoded, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()
	return vertexAccessToken(context.Background(), providerEnvMap())
}

// D341 caps a successful remote JSON body and fails as a protocol error once it
// exceeds the limit: 1 MiB for OAuth documents. The authorized-user token
// response is such a document, so an oversized one must report the size limit
// instead of reading a silent 1 MiB prefix, which accepts the token when the
// JSON ends before the cap (A426).
func TestVertexADCTokenResponseOverTheLimitIsAProtocolError(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body := `{"access_token":"adc-token","expires_in":3600}` + strings.Repeat(" ", (1<<20)+1)
		_, _ = writer.Write([]byte(body))
	}))
	defer tokenServer.Close()

	token, err := vertexADCTokenAccess(t, tokenServer.URL)
	if err == nil {
		t.Fatalf("an oversized token response was accepted (token %q)", token)
	}
	if !errors.Is(err, boundedio.ErrTooLarge) {
		t.Fatalf("err = %v; want the size-limit error (D341)", err)
	}
}

// A token response cut off mid-body must surface the transport error, as the
// instance-metadata path in the same file does, instead of a JSON parse error
// that hides whether the network dropped or the server sent garbage (A426).
func TestVertexADCTokenResponseCutOffSurfacesTheReadError(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Length", "64")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"access_token":"adc`))
	}))
	defer tokenServer.Close()

	token, err := vertexADCTokenAccess(t, tokenServer.URL)
	if err == nil {
		t.Fatalf("a cut-off token response was accepted (token %q)", token)
	}
	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("err = %v; want the transport error", err)
	}
}

// Control for the cap split: an error response still reports its status and
// body, which the token path builds before it looks at a successful document.
func TestVertexADCTokenErrorResponseReportsStatusAndBody(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer tokenServer.Close()

	_, err := vertexADCTokenAccess(t, tokenServer.URL)
	if err == nil {
		t.Fatal("a rejected token response produced a token")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("err = %v; want the status and body", err)
	}
}

// Reference: pi v1.4.0 (4a080252), api/google-generative-ai.ts and
// api/google-vertex.ts buildParams/stream, plus @google/genai 2.21.0 wire
// serializers. Fixtures capture HTTP bodies, not the SDK's input params.
// Regenerate: node --experimental-strip-types testdata/google-wire/generate.mjs ../pi > testdata/google-wire/golden.json
func TestGoogleRequestsMatchSDKWire(t *testing.T) {
	data, err := os.ReadFile("testdata/google-wire/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		API         Api               `json:"api"`
		Name        string            `json:"name"`
		Messages    []json.RawMessage `json:"messages"`
		Options     GoogleOptions     `json:"options"`
		Body        string            `json:"body"`
		Payload     string            `json:"payload"`
		Replacement json.RawMessage   `json:"replacement"`
		Path        string            `json:"path"`
		Error       string            `json:"error"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(string(fixture.API)+"/"+fixture.Name, func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				paths <- request.URL.Path
				body, _ := io.ReadAll(request.Body)
				bodies <- string(body)
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}\n\n")
			}))
			defer server.Close()
			model := &Model{API: fixture.API, ID: "gemini-3-pro", Provider: "google", BaseURL: server.URL + "/v1",
				Reasoning: true, MaxTokens: 1000, ContextWindow: 10000, Input: []string{"text", "image"}}
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			options := fixture.Options
			payloads := make(chan string, 1)
			options.OnPayload = func(payload json.RawMessage, _ *Model) json.RawMessage {
				payloads <- string(payload)
				return fixture.Replacement
			}
			options.APIKey = "token"
			options.Env = ProviderEnv{}
			var stream *AssistantMessageEventStream
			if fixture.API == APIGoogleVertex {
				model.Provider = "google-vertex"
				stream = StreamGoogleVertex(model, TranscriptContext{Messages: messages}, &GoogleVertexOptions{
					GoogleOptions: options, Project: "project", Location: "global",
				})
			} else {
				stream = StreamGoogleGenerativeAI(model, TranscriptContext{Messages: messages}, &options)
			}
			result, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := <-payloads; got != fixture.Payload {
				t.Errorf("payload = %s\nwant      %s", got, fixture.Payload)
			}
			if fixture.Error != "" {
				// The SDK rejects config members the API does not support while
				// serializing, so the hook runs and no request is sent.
				if result.StopReason != StopError || result.ErrorMessage == nil || *result.ErrorMessage != fixture.Error {
					t.Fatalf("result = %+v, want error %q", result, fixture.Error)
				}
				select {
				case path := <-paths:
					t.Fatalf("rejected config sent a request: %s", path)
				default:
				}
				return
			}
			if result.StopReason != StopStop {
				t.Fatalf("stopReason = %s, error = %v", result.StopReason, result.ErrorMessage)
			}
			if got := <-paths; got != fixture.Path {
				t.Errorf("path = %s, want %s", got, fixture.Path)
			}
			if got := <-bodies; got != fixture.Body {
				t.Fatalf("body = %s\nwant   %s", got, fixture.Body)
			}
		})
	}
}

// Since the SDK 2.32 upgrade (@google/genai, v1.6.0) the Developer API accepts
// `labels` and sends them as a top-level body member before generationConfig;
// `labels: null` is omitted, and modelArmorConfig stays Vertex-only. The
// expectation was produced by the pinned reference's stream with an onPayload
// replacement.
func TestGoogleDeveloperRequestCarriesLabels(t *testing.T) {
	replacement := json.RawMessage(`{"model":"gemini-3-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"config":{"labels":{"audit":"enabled"},"temperature":0.5}}`)
	_, body, err := googleSDKRequest(replacement, APIGoogleGenerativeAI)
	if err != nil {
		t.Fatalf("labels rejected: %v", err)
	}
	want := `{"contents":[{"parts":[{"text":"hello"}],"role":"user"}],"labels":{"audit":"enabled"},"generationConfig":{"temperature":0.5}}`
	if string(body) != want {
		t.Fatalf("body = %s\nwant %s", body, want)
	}

	nullLabels := json.RawMessage(`{"model":"gemini-3-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"config":{"labels":null}}`)
	_, body, err = googleSDKRequest(nullLabels, APIGoogleGenerativeAI)
	if err != nil {
		t.Fatalf("null labels rejected: %v", err)
	}
	if strings.Contains(string(body), "labels") {
		t.Fatalf("null labels were sent: %s", body)
	}

	// modelArmorConfig is still Vertex-only.
	armor := json.RawMessage(`{"model":"gemini-3-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"config":{"modelArmorConfig":{}}}`)
	if _, _, err := googleSDKRequest(armor, APIGoogleGenerativeAI); err == nil ||
		!strings.Contains(err.Error(), "modelArmorConfig parameter is only supported in Gemini Enterprise Agent Platform mode") {
		t.Fatalf("modelArmorConfig err = %v, want the Developer API rejection", err)
	}

	// generationOutputConfig is rejected right after modelArmorConfig by the
	// Developer serializer (SDK generateContentConfigToMldev), and by no other
	// member's check, so it is Vertex-only as well. Vertex itself accepts it.
	output := json.RawMessage(`{"model":"gemini-3-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"config":{"generationOutputConfig":{}}}`)
	if _, _, err := googleSDKRequest(output, APIGoogleGenerativeAI); err == nil ||
		!strings.Contains(err.Error(), "generationOutputConfig parameter is only supported in Gemini Enterprise Agent Platform mode") {
		t.Fatalf("generationOutputConfig err = %v, want the Developer API rejection", err)
	}
	// The Vertex serializer does not reject it.
	if _, _, err := googleSDKRequest(output, APIGoogleVertex); err != nil {
		t.Fatalf("the Vertex API rejected generationOutputConfig: %v", err)
	}

	// The Vertex serializer rejects both enableEnhancedCivicAnswers and store,
	// in that order, as Developer-API-only members
	// (SDK generateContentConfigToVertex). The config map is free-form, so a hook
	// replacement can carry either.
	for _, key := range []string{"enableEnhancedCivicAnswers", "store"} {
		raw := json.RawMessage(`{"model":"gemini-3-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"config":{"` + key + `":{}}}`)
		if _, _, err := googleSDKRequest(raw, APIGoogleVertex); err == nil ||
			!strings.Contains(err.Error(), key+" parameter is only supported in Gemini Developer API mode") {
			t.Fatalf("%s err = %v, want the Vertex API rejection", key, err)
		}
	}
}
