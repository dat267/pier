package durable

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/define.ts and the hook surface.

func TestDefineConstructors(t *testing.T) {
	extension := DefineExtension(Extension{Name: "a"})
	if extension.Name != "a" {
		t.Fatalf("extension = %+v", extension)
	}
	tool := DefineTool(testTool("x", "d"))
	if tool.Name != "x" {
		t.Fatalf("tool = %+v", tool)
	}
	tag := false
	section := Section("key", func(PromptInput, chord.Context) (string, bool, error) { return "v", true, nil }, &tag)
	if section.Key != "key" || section.Tag == nil || *section.Tag {
		t.Fatalf("section = %+v", section)
	}
	registration := Hook(GenerationTask, GenerationHooks{})
	if registration.Task != RunTaskKind {
		t.Fatalf("registration = %+v", registration)
	}
	wrapped := WrapTool(tool, func(ToolRegistration) ToolRegistration { return tool })
	if wrapped.Tool != "x" || wrapped.WrapTool == nil {
		t.Fatalf("wrap = %+v", wrapped)
	}
	sectionWrap := WrapSection("key", func(section PromptSection) PromptSection { return section })
	if sectionWrap.Section != "key" || sectionWrap.WrapSection == nil {
		t.Fatalf("wrap = %+v", sectionWrap)
	}
}

func TestHookRegistrationCarriesTypedHandlers(t *testing.T) {
	message := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}
	hooks := GenerationHooks{
		BeforeRequest: func(request GenerationRequest, api HookApi, ctx chord.Context) (*GenerationRequest, error) {
			return &request, nil
		},
		AfterTools: func(assistant Id, results []Id, api HookApi, ctx chord.Context) error { return nil },
	}
	registration := Hook(GenerationTask, hooks)
	typed, ok := registration.Handlers.(GenerationHooks)
	if !ok || typed.BeforeRequest == nil || typed.AfterTools == nil {
		t.Fatalf("handlers = %+v", registration.Handlers)
	}
	request, err := typed.BeforeRequest(GenerationRequest{Messages: []ai.Message{message}}, nil, context.Background())
	if err != nil || len(request.Messages) != 1 {
		t.Fatalf("request = %+v, %v", request, err)
	}
}
