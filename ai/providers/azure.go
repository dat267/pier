package providers

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of packages/ai/src/providers/azure.ts. Azure serves the Responses API and Foundry
// Chat Completions deployments, and both need the endpoint resolved per request and the
// deployment name sent in place of the catalog model id, so the second API is registered
// through this wrapper rather than bare.

// azureStreams resolves the Azure endpoint and deployment around an OpenAI-compatible API
// implementation.
type azureStreams struct{ inner ai.ProviderStreams }

func (s azureStreams) Stream(model *ai.Model, context ai.TranscriptContext, options *ai.StreamOptions) *ai.AssistantMessageEventStream {
	resolved, next, err := resolveAzureRequest(model, options)
	if err != nil {
		return azureErrorStream(model, err)
	}
	return s.inner.Stream(resolved, context, next)
}

func (s azureStreams) StreamSimple(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	var base *ai.StreamOptions
	if options != nil {
		base = &options.StreamOptions
	}
	resolved, next, err := resolveAzureRequest(model, base)
	if err != nil {
		return azureErrorStream(model, err)
	}
	simple := &ai.SimpleStreamOptions{}
	if options != nil {
		*simple = *options
	}
	if next != nil {
		simple.StreamOptions = *next
	}
	return s.inner.StreamSimple(resolved, context, simple)
}

// resolveAzureRequest returns the model with its resolved endpoint, and options whose
// OnPayload writes the deployment name into the request body. The catalog id stays the
// model's identity, because cost, thinking levels and the picker are all keyed by it; only
// the wire payload names the deployment.
func resolveAzureRequest(model *ai.Model, options *ai.StreamOptions) (*ai.Model, *ai.StreamOptions, error) {
	endpoint := &ai.AzureOpenAIResponsesOptions{}
	if options != nil {
		endpoint.Env = options.Env
	}
	baseURL, _, err := ai.ResolveAzureConfig(model, endpoint)
	if err != nil {
		return nil, nil, err
	}
	deployment := ai.ResolveAzureDeploymentName(model, endpoint)
	resolved := *model
	resolved.BaseURL = baseURL
	next := &ai.StreamOptions{}
	if options != nil {
		*next = *options
	}
	previous := next.OnPayload
	next.OnPayload = func(payload json.RawMessage, payloadModel *ai.Model) json.RawMessage {
		rewritten := withPayloadModel(payload, deployment)
		if previous != nil {
			return previous(rewritten, payloadModel)
		}
		return rewritten
	}
	return &resolved, next, nil
}

// azureErrorStream reports a request whose endpoint could not be resolved the way the API
// reports a failure, through the stream. Upstream resolves inside `lazyStream` for the same
// reason: an unconfigured endpoint fails the request instead of throwing out of stream().
func azureErrorStream(model *ai.Model, err error) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	message := err.Error()
	failure := &ai.AssistantMessage{
		API: model.API, Provider: model.Provider, Model: model.ID,
		Usage: ai.Usage{Cost: ai.UsageCost{}}, StopReason: ai.StopError,
		ErrorMessage: &message, Timestamp: time.Now().UnixMilli(),
	}
	go func() {
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopError, Error: failure})
		stream.End(&failure)
	}()
	return stream
}

// withPayloadModel sets the top-level "model" member of a JSON object and leaves every other
// byte as it was. Upstream spreads the parsed payload (`{ ...payload, model }`), which keeps
// the member where it was and the rest of the document untouched; a map round trip would
// reorder every key, and this payload is wire data.
func withPayloadModel(payload []byte, name string) []byte {
	encoded, err := json.Marshal(name)
	if err != nil {
		return payload
	}
	start, end, ok := jsonMemberSpan(payload, "model")
	if !ok {
		return payload
	}
	out := make([]byte, 0, len(payload)+len(encoded))
	out = append(out, payload[:start]...)
	out = append(out, encoded...)
	out = append(out, payload[end:]...)
	return out
}

// jsonMemberSpan reports the byte range of a top-level member's value.
func jsonMemberSpan(payload []byte, member string) (int, int, bool) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return 0, 0, false
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return 0, 0, false
		}
		key, _ := keyToken.(string)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return 0, 0, false
		}
		if key != member {
			continue
		}
		// The decoded raw message is the value exactly, so it ends where the decoder is and
		// starts its own length before that.
		end := int(decoder.InputOffset())
		return end - len(value), end, true
	}
	return 0, 0, false
}
