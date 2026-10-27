package ai

import (
	"context"
	"testing"
	"time"
)

type setupCancelStreams struct{}

func (setupCancelStreams) Stream(model *Model, _ TranscriptContext, _ *StreamOptions) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	message := &AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: StopStop}
	stream.End(&message)
	return stream
}

func (s setupCancelStreams) StreamSimple(model *Model, context TranscriptContext, options *SimpleStreamOptions) *AssistantMessageEventStream {
	return s.Stream(model, context, &options.StreamOptions)
}

// A request canceled while auth resolves is an abort. Upstream's applyAuth calls
// signal.throwIfAborted, and the port reports its canonical abort text instead
// of Go's "context canceled" (D256).
func TestModelsStreamSetupFailureOnCancelReportsAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	models := CreateModels(nil)
	models.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "p", Name: "provider",
		Auth: ProviderAuth{OAuth: &OAuthAuth{
			Name: "test",
			Refresh: func(_ *OAuthCredential, _ context.Context) (*OAuthCredential, error) {
				cancel()
				return &OAuthCredential{
					Access: "a2", Refresh: "r2", Expires: time.Now().Add(time.Hour).UnixMilli()}, nil
			},
			ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
				return &ModelAuth{APIKey: credential.Access}, nil
			},
		}},
		Models: []*Model{{ID: "m", API: APIOpenAICompletions, Provider: "p"}},
		Single: setupCancelStreams{},
	}))
	if _, err := models.credentials.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Access: "a1", Refresh: "r1", Expires: 0,
		}}}, nil
	}, context.Background()); err != nil {
		t.Fatal(err)
	}
	model := models.GetModel("p", "m")
	if model == nil {
		t.Fatal("model missing")
	}
	message, _ := models.Stream(model, Context{}, &ModelsStreamOptions{
		Ctx: ctx,
	}).Result(context.Background())
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		*message.ErrorMessage != RequestAbortedMessage {
		t.Fatalf("message = %+v; want %q", message, RequestAbortedMessage)
	}
}
