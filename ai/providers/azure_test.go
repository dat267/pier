package providers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

type azureRecordingStreams struct {
	model   *ai.Model
	options *ai.StreamOptions
	payload json.RawMessage
}

func (r *azureRecordingStreams) capture(model *ai.Model, options *ai.StreamOptions) *ai.AssistantMessageEventStream {
	r.model = model
	r.options = options
	if options != nil && options.OnPayload != nil {
		r.payload = options.OnPayload(
			json.RawMessage(`{"model":"catalog-id","messages":[{"role":"user","content":"hi"}],"stream":true}`), model)
	}
	return ai.NewAssistantMessageEventStream()
}

func (r *azureRecordingStreams) Stream(model *ai.Model, context ai.TranscriptContext, options *ai.StreamOptions) *ai.AssistantMessageEventStream {
	return r.capture(model, options)
}

func (r *azureRecordingStreams) StreamSimple(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	if options == nil {
		return r.capture(model, nil)
	}
	return r.capture(model, &options.StreamOptions)
}

// Azure serves Foundry Chat Completions deployments, where the request must name the
// deployment rather than the catalog model, and the endpoint is resolved per request
// (upstream azureStreams, provider/azure.ts).
func TestAzureStreamsResolveEndpointAndDeployment(t *testing.T) {
	inner := &azureRecordingStreams{}
	streams := azureStreams{inner: inner}
	model := &ai.Model{
		ID: "gpt-4o", Provider: "azure", API: ai.APIOpenAICompletions,
		BaseURL: "https://myresource.openai.azure.com",
	}
	options := &ai.StreamOptions{
		Env: ai.ProviderEnv{"AZURE_OPENAI_DEPLOYMENT_NAME_MAP": "gpt-4o=my-deployment"},
	}
	streams.Stream(model, ai.TranscriptContext{}, options)

	if inner.model == nil {
		t.Fatal("the inner implementation was not called")
	}
	if got, want := inner.model.BaseURL, "https://myresource.openai.azure.com/openai/v1"; got != want {
		t.Fatalf("base url = %q, want %q", got, want)
	}
	// The payload names the deployment; every other byte is what the caller built.
	want := `{"model":"my-deployment","messages":[{"role":"user","content":"hi"}],"stream":true}`
	if got := string(inner.payload); got != want {
		t.Fatalf("payload = %s\n           want %s", got, want)
	}
	// The catalog model keeps its identity: everything else is keyed by the id.
	if model.ID != "gpt-4o" || model.BaseURL != "https://myresource.openai.azure.com" {
		t.Fatalf("the caller's model was mutated: %+v", model)
	}
	// The caller's own payload hook runs, and sees the rewritten payload.
	inner.payload = nil
	seen := ""
	streams.Stream(model, ai.TranscriptContext{}, &ai.StreamOptions{
		Env: options.Env,
		OnPayload: func(payload json.RawMessage, model *ai.Model) json.RawMessage {
			seen = string(payload)
			return json.RawMessage(`{"replaced":true}`)
		},
	})
	if !json.Valid(inner.payload) || string(inner.payload) != `{"replaced":true}` {
		t.Fatalf("the caller's hook did not win: %s", inner.payload)
	}
	if wantDeployment := `"model":"my-deployment"`; !contains(seen, wantDeployment) {
		t.Fatalf("the caller's hook saw %s, want it to contain %s", seen, wantDeployment)
	}
}

// A missing endpoint is reported through the stream, the way upstream's lazyStream defers
// it, rather than panicking out of the API.
func TestAzureStreamsReportAnUnresolvedEndpoint(t *testing.T) {
	streams := azureStreams{inner: &azureRecordingStreams{}}
	model := &ai.Model{ID: "gpt-4o", Provider: "azure", API: ai.APIOpenAICompletions}
	stream := streams.Stream(model, ai.TranscriptContext{}, &ai.StreamOptions{})
	events := stream.Events(context.Background())
	if len(events) == 0 {
		t.Fatal("no event")
	}
	event := events[0]
	if event.Type != ai.EventError || event.Error == nil || event.Error.StopReason != ai.StopError {
		t.Fatalf("event = %+v", event)
	}
}

// The rewrite has to be byte-faithful: these payloads are wire data, and upstream's
// `{ ...payload, model }` leaves every other member and its order alone.
func TestWithPayloadModelKeepsTheRestByteForByte(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "first member",
			payload: `{"model":"a","messages":[]}`,
			want:    `{"model":"b","messages":[]}`,
		},
		{
			name:    "later member keeps its place",
			payload: `{"messages":[],"model":"a","stream":true}`,
			want:    `{"messages":[],"model":"b","stream":true}`,
		},
		{
			name:    "whitespace is preserved",
			payload: "{ \"model\" : \"a\" ,\n  \"stream\": true }",
			want:    "{ \"model\" : \"b\" ,\n  \"stream\": true }",
		},
		{
			name:    "no model member",
			payload: `{"messages":[]}`,
			want:    `{"messages":[]}`,
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := string(withPayloadModel([]byte(testCase.payload), "b")); got != testCase.want {
				t.Fatalf("got  %s\nwant %s", got, testCase.want)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
