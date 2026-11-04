package coding

import (
	"context"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi f9a26365 core/model-runtime.ts prepareRequest passes signal
// into getAuth, and preserves it in the returned provider request options.
func TestPreparedRequestOptionsKeepCallerCancellation(t *testing.T) {
	runtime := runtimeWithProviders(t, stubProvider("alpha"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prepared, err := runtime.PrepareRequest(runtime.GetModel("alpha", "alpha-model"), "synthetic-key", nil, ctx, nil, nil)
	if err != nil || prepared == nil {
		t.Fatalf("live request prepared=%v, error=%v", prepared != nil, err)
	}
	cancel()
	for name, requestCtx := range map[string]context.Context{
		"stream": prepared.StreamOptions.Ctx, "simple": prepared.SimpleOptions.Ctx,
	} {
		if requestCtx == nil || !errors.Is(requestCtx.Err(), context.Canceled) {
			t.Errorf("%s request options lost caller cancellation", name)
		}
	}
}

func TestPrepareRequestStopsCanceledAuthResolution(t *testing.T) {
	runtime := runtimeWithProviders(t, stubProvider("alpha"))
	model := runtime.GetModel("alpha", "alpha-model")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transformed := false
	prepared, err := runtime.PrepareRequest(model, "synthetic-key", nil, ctx, nil, func(headers ai.ProviderHeaders) ai.ProviderHeaders {
		transformed = true
		return headers
	})
	if !errors.Is(err, context.Canceled) || prepared != nil {
		t.Fatalf("canceled request prepared=%v, error=%v", prepared != nil, err)
	}
	if transformed {
		t.Fatal("canceled auth reached the header transform")
	}
	if control, err := runtime.PrepareRequest(model, "synthetic-key", nil, context.Background(), nil, nil); err != nil || control == nil {
		t.Fatalf("live request prepared=%v, error=%v", control != nil, err)
	}
}
