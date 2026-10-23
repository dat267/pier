package ai

import "testing"

// Reference: pi v1.4.0 utils/identity.ts withClientIdentity delegates each
// request method to that same method, with a copied options object.
type identityRequestRegistry struct {
	ModelRegistry
	called   string
	identity *ClientIdentity
}

func (m *identityRequestRegistry) record(method string, options *ModelsStreamOptions) {
	m.called, m.identity = method, nil
	if options != nil {
		m.identity = options.Identity
	}
}
func (m *identityRequestRegistry) Stream(_ *Model, _ Context, o *ModelsStreamOptions) *AssistantMessageEventStream {
	m.record("stream", o)
	return doneStream()
}
func (m *identityRequestRegistry) Complete(_ *Model, _ Context, o *ModelsStreamOptions) (*AssistantMessage, error) {
	m.record("complete", o)
	return &AssistantMessage{StopReason: StopStop}, nil
}
func (m *identityRequestRegistry) StreamSimple(_ *Model, _ Context, o *ModelsSimpleStreamOptions) *AssistantMessageEventStream {
	var options ModelsStreamOptions
	if o != nil {
		options.StreamOptions = o.StreamOptions
	}
	m.record("simple", &options)
	return doneStream()
}
func (m *identityRequestRegistry) CompleteSimple(_ *Model, _ Context, o *ModelsSimpleStreamOptions) (*AssistantMessage, error) {
	var options ModelsStreamOptions
	if o != nil {
		options.StreamOptions = o.StreamOptions
	}
	m.record("completeSimple", &options)
	return &AssistantMessage{StopReason: StopStop}, nil
}
func (m *identityRequestRegistry) StreamDeferred(_ *Model, _ *DeferredHandle, o *ModelsStreamOptions) *AssistantMessageEventStream {
	m.record("streamDeferred", o)
	return doneStream()
}
func (m *identityRequestRegistry) FetchDeferred(_ *Model, _ *DeferredHandle, o *ModelsStreamOptions) (*AssistantMessage, error) {
	m.record("fetchDeferred", o)
	return &AssistantMessage{StopReason: StopStop}, nil
}
func (m *identityRequestRegistry) CancelDeferred(_ *Model, _ *DeferredHandle, o *ModelsStreamOptions) error {
	m.record("cancelDeferred", o)
	return nil
}

func TestWithClientIdentityDelegatesRequestMethods(t *testing.T) {
	for _, method := range []string{"stream", "complete", "simple", "completeSimple", "streamDeferred", "fetchDeferred", "cancelDeferred"} {
		t.Run(method, func(t *testing.T) {
			source := &identityRequestRegistry{ModelRegistry: CreateModels(nil)}
			wrapped := WithClientIdentity(source, &ClientIdentity{UserAgent: "short"})
			model, handle := &Model{}, &DeferredHandle{}
			var err error
			switch method {
			case "stream":
				_, err = wrapped.Stream(model, Context{}, nil).Result(bgCtx())
			case "complete":
				_, err = wrapped.Complete(model, Context{}, nil)
			case "simple":
				_, err = wrapped.StreamSimple(model, Context{}, nil).Result(bgCtx())
			case "completeSimple":
				_, err = wrapped.CompleteSimple(model, Context{}, nil)
			case "streamDeferred":
				_, err = wrapped.StreamDeferred(model, handle, nil).Result(bgCtx())
			case "fetchDeferred":
				_, err = wrapped.FetchDeferred(model, handle, nil)
			case "cancelDeferred":
				err = wrapped.CancelDeferred(model, handle, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if source.called != method || source.identity == nil || source.identity.UserAgent != "short" {
				t.Fatalf("delegated method = %q, identity = %+v", source.called, source.identity)
			}
		})
	}
}
