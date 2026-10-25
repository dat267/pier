package providers

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 providers/azure.ts withDeploymentName returns the rewritten params
// when the caller's hook returns undefined/null (nil in the Go hook API).
func TestAzureDeploymentSurvivesNoPayloadReplacement(t *testing.T) {
	inner := &azureRecordingStreams{}
	streams := azureStreams{inner: inner}
	model := &ai.Model{
		ID: "gpt-4o", Provider: "azure", API: ai.APIOpenAICompletions,
		BaseURL: "https://fixture.openai.azure.com",
	}
	streams.Stream(model, ai.TranscriptContext{}, &ai.StreamOptions{
		Env:       ai.ProviderEnv{"AZURE_OPENAI_DEPLOYMENT_NAME_MAP": "gpt-4o=fixture-deployment"},
		OnPayload: func(json.RawMessage, *ai.Model) json.RawMessage { return nil },
	})
	want := `{"model":"fixture-deployment","messages":[{"role":"user","content":"hi"}],"stream":true}`
	if got := string(inner.payload); got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

// pi v1.4.0 providers/azure.ts withDeploymentName spreads the payload then
// JSON.stringify sends model without HTML escaping. The deployment map is
// caller configuration, not an SDK serializer that escapes '<', '>' or '&'.
func TestAzureDeploymentRewriteDoesNotHTMLEscape(t *testing.T) {
	inner := &azureRecordingStreams{}
	streams := azureStreams{inner: inner}
	model := &ai.Model{
		ID: "gpt-4o", Provider: "azure", API: ai.APIOpenAICompletions,
		BaseURL: "https://fixture.openai.azure.com",
	}
	streams.Stream(model, ai.TranscriptContext{}, &ai.StreamOptions{
		Env: ai.ProviderEnv{"AZURE_OPENAI_DEPLOYMENT_NAME_MAP": "gpt-4o=deployment<&>"},
		OnPayload: func(payload json.RawMessage, _ *ai.Model) json.RawMessage {
			want := `{"model":"deployment<&>","messages":[{"role":"user","content":"hi"}],"stream":true}`
			if string(payload) != want {
				t.Errorf("payload = %s, want %s", payload, want)
			}
			return payload
		},
	})
}
