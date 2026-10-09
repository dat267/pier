package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of configurable partial throttling from packages/durable/src/harness/generation.ts at pi v1.1.0 commit 674d64f09.
func TestGenerationPartialIntervalUsesProgressSettings(t *testing.T) {
	models := partialTestModels()
	model := models.GetModel("progress", "test")
	if model == nil {
		t.Fatal("test model missing")
	}
	settings := ResolveSettings(&HarnessSettings{Progress: &ProgressPolicyChange{PartialIntervalMs: intPointer(0)}})
	runtime := &partialSettingsRuntime{models: models, settings: settings}
	_, err := streamGenerationResponse(runtime, model, nil, ai.SimpleStreamOptions{}, 1, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.commits != 3 {
		t.Fatalf("partial commits = %d, want 3 with zero interval", runtime.commits)
	}
}

// Port of harness/tool.ts outputIntervalMs wiring at pi v1.1.0 commit 674d64f09.
func TestToolProgressUsesOutputIntervalSetting(t *testing.T) {
	runtime := &outputIntervalRuntime{settings: Settings{Progress: ProgressPolicy{OutputIntervalMs: 0}}}
	reported := &reportedTool{output: NewOutputBuffer(OutputLimits{MaxBytes: 100, MaxLines: 10})}
	progress := publishToolProgress(runtime, reported, context.Background(), 0)
	defer progress.Stop()
	reported.output.Push("first")
	if err := progress.MarkAndWait().Wait(); err != nil {
		t.Fatal(err)
	}
	reported.output.Push("second")
	started := time.Now()
	if err := progress.MarkAndWait().Wait(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("zero output interval delayed progress for %v", elapsed)
	}
	if got := runtime.writes.Load(); got != 2 {
		t.Fatalf("progress commits = %d, want 2", got)
	}
}

type outputIntervalRuntime struct {
	TaskRuntime
	settings Settings
	writes   atomic.Int32
}

func (r *outputIntervalRuntime) Settings() Settings      { return r.settings }
func (r *outputIntervalRuntime) Signal() context.Context { return context.Background() }
func (r *outputIntervalRuntime) Report(error)            {}
func (r *outputIntervalRuntime) Commit(func(*Transaction, RunningTask) (*NextTaskState, error), chord.Context) error {
	r.writes.Add(1)
	return nil
}

type partialSettingsRuntime struct {
	TaskRuntime
	models   *ai.Models
	settings Settings
	commits  int
}

func (r *partialSettingsRuntime) Models() *ai.Models      { return r.models }
func (r *partialSettingsRuntime) Settings() Settings      { return r.settings }
func (r *partialSettingsRuntime) Signal() context.Context { return context.Background() }
func (r *partialSettingsRuntime) Report(error)            {}
func (r *partialSettingsRuntime) Commit(func(*Transaction, RunningTask) (*NextTaskState, error), chord.Context) error {
	r.commits++
	return nil
}

type partialTestStreams struct{}

func (partialTestStreams) Stream(*ai.Model, ai.TranscriptContext, *ai.StreamOptions) *ai.AssistantMessageEventStream {
	return partialTestStream()
}

func (partialTestStreams) StreamSimple(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	return partialTestStream()
}

func partialTestStream() *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	for _, text := range []string{"one", "two", "three"} {
		partial := &ai.AssistantMessage{
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopPending,
		}
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Partial: partial})
	}
	final := &ai.AssistantMessage{StopReason: ai.StopStop}
	stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: final})
	return stream
}

func partialTestModels() *ai.Models {
	models := ai.CreateModels(nil)
	provider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "progress",
		Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{
			Name: "progress",
			Resolve: func(ai.AuthResolveInput) (*ai.AuthResult, error) {
				return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "test"}, Source: "test"}, nil
			},
		}},
		Models: []*ai.Model{{
			ID: "test", Name: "test", Provider: "progress", API: ai.APIOpenAICompletions,
			Input: []string{"text"}, ContextWindow: 1024, MaxTokens: 256,
		}},
		Single: partialTestStreams{},
	})
	models.SetProvider(provider)
	return models
}
