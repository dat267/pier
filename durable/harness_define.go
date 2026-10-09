package durable

import (
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/define.ts and the hook surface of harness/types.ts.

// DefineExtension types an extension (the identity function).
func DefineExtension(extension Extension) Extension { return extension }

// DefineTool types a tool (the identity function).
func DefineTool(tool ToolRegistration) ToolRegistration { return tool }

// Section builds a prompt section; tagged unless tag is false.
func Section(key string, render func(input PromptInput, ctx chord.Context) (string, bool, error), tag *bool) PromptSection {
	return PromptSection{Key: key, Render: render, Tag: tag}
}

// Hook builds the hook registration of a task's handlers.
func Hook(task Task, handlers any) HookRegistration {
	return HookRegistration{Task: task.Definition.Name, Handlers: handlers}
}

// WrapTool wraps the tool named like tool wherever the wrapping extension is
// selected.
func WrapTool(tool ToolRegistration, wrapper func(ToolRegistration) ToolRegistration) Wrap {
	return Wrap{Tool: tool.Name, WrapTool: wrapper}
}

// WrapSection wraps a section key wherever the wrapping extension is selected.
func WrapSection(key string, wrapper func(PromptSection) PromptSection) Wrap {
	return Wrap{Section: key, WrapSection: wrapper}
}

// HookApi is what a hook may use: committed reads and the asking task's memos,
// which hooks and the task share.
type HookApi interface {
	TaskID() Id
	ConversationID() Id
	// Models is the HarnessOptions model catalog shared with generation.
	Models() *ai.Models
	Memo(ctx chord.Context, name string) (chord.JsonValue, bool, error)
	MemoSet(ctx chord.Context, name string, candidate chord.JsonValue) (chord.JsonValue, error)
}

// GenerationRequest is what a generation hook's before-request sees and may
// replace.
type GenerationRequest struct {
	Messages []ai.Message
}

// YieldDecision is a generation hook's decision on a final answer; the first
// Continue appends a user message and continues the run.
type YieldDecision struct {
	Continue *UserInput
}

// GenerationHooks are the built-in generation task's hooks.
type GenerationHooks struct {
	BeforeRequest func(request GenerationRequest, api HookApi, ctx chord.Context) (*GenerationRequest, error)
	AfterResponse func(message *ai.AssistantMessage, api HookApi, ctx chord.Context) error
	OnYield       func(answer *ai.AssistantMessage, api HookApi, ctx chord.Context) (*YieldDecision, error)
	AfterTools    func(assistant Id, results []Id, api HookApi, ctx chord.Context) error
}

// ToolDecision is a tool hook's decision before a call; the first block wins,
// otherwise the arguments replace the call's.
type ToolDecision struct {
	Arguments JsonObject
	Block     *string
}

// ToolHooks are the built-in tool task's hooks.
type ToolHooks struct {
	BeforeTool func(call ai.ToolCall, api HookApi, ctx chord.Context) (*ToolDecision, error)
	AfterTool  func(call ai.ToolCall, result ToolExecutionResult, api HookApi, ctx chord.Context) (*ToolExecutionResult, error)
}

// CompactionHookRequest is what a compaction hook's before-compact sees.
type CompactionHookRequest struct {
	Reason       CompactionReason
	Entries      []EntryRecord
	Messages     []ai.Message
	FirstKept    Id
	Instructions *string
}

// CompactionDecision is a compaction hook's decision; the first one wins.
type CompactionDecision struct {
	Decline bool
	Summary *string
}

// CompactionHooks are the built-in compaction task's hooks.
type CompactionHooks struct {
	BeforeCompact func(compaction CompactionHookRequest, api HookApi, ctx chord.Context) (*CompactionDecision, error)
}
