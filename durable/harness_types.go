package durable

import (
	"encoding/json"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the harness types the entry helpers reference (harness/types.ts):
// tool diagnostics and the compaction reason.

// ToolDiagnostic is one structured tool diagnostic (upstream ToolDiagnostic).
type ToolDiagnostic struct {
	// Severity is "info", "warn" or "error".
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	Code     *string `json:"code,omitempty"`
}

// Diagnostic severities.
const (
	DiagnosticInfo  = "info"
	DiagnosticWarn  = "warn"
	DiagnosticError = "error"
)

// CompactionReason is why a compaction ran (upstream CompactionReason).
type CompactionReason = string

// Compaction reasons.
const (
	CompactionManual    CompactionReason = "manual"
	CompactionThreshold CompactionReason = "threshold"
	CompactionOverflow  CompactionReason = "overflow"
)

// Port of harness/types.ts: the agent, registry, extension and tool surface
// the harness modules share.

// Optional models an upstream `T | null | undefined` change field: absent,
// present-and-null, or present-and-set.
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// SetOf is a present optional carrying a value.
func SetOf[T any](value T) Optional[T] { return Optional[T]{Set: true, Value: value} }

// NullOf is a present optional that clears the field.
func NullOf[T any]() Optional[T] { return Optional[T]{Set: true, Null: true} }

// ModelRef is a provider and model id resolved through pi-ai `Models`.
type ModelRef struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// Tool execution modes (upstream ToolExecutionMode).
const (
	ToolExecutionParallel   = "parallel"
	ToolExecutionSequential = "sequential"
)

// CompactionPolicy is the automatic compaction configuration.
type CompactionPolicy struct {
	// Enabled gates threshold and overflow compaction.
	Enabled bool `json:"enabled"`
	// ReserveTokens is the room kept free for the answer.
	ReserveTokens int `json:"reserveTokens"`
	// KeepRecentTokens is the approximate size kept verbatim by a summary.
	KeepRecentTokens int `json:"keepRecentTokens"`
	// BackgroundTokens starts background compaction; 0 disables it.
	BackgroundTokens int `json:"backgroundTokens"`
}

// ConversationRetryPolicy is the durable generation-attempt retry policy; its
// JSON shape is pi-ai `RetryPolicy`.
type ConversationRetryPolicy struct {
	Enabled         bool `json:"enabled"`
	MaxRetries      int  `json:"maxRetries"`
	BaseDelayMs     int  `json:"baseDelayMs"`
	MaxAgentDelayMs *int `json:"maxAgentDelayMs,omitempty"`
}

// RetryPolicyChange is the host's partial retry policy (`Partial<…>`).
type RetryPolicyChange struct {
	Enabled         *bool `json:"enabled,omitempty"`
	MaxRetries      *int  `json:"maxRetries,omitempty"`
	BaseDelayMs     *int  `json:"baseDelayMs,omitempty"`
	MaxAgentDelayMs *int  `json:"maxAgentDelayMs,omitempty"`
}

// CompactionPolicyChange is the host's partial compaction policy.
type CompactionPolicyChange struct {
	Enabled          *bool `json:"enabled,omitempty"`
	ReserveTokens    *int  `json:"reserveTokens,omitempty"`
	KeepRecentTokens *int  `json:"keepRecentTokens,omitempty"`
	BackgroundTokens *int  `json:"backgroundTokens,omitempty"`
}

// ConversationStreamOptions is the curated pi-ai request option set.
type ConversationStreamOptions struct {
	Transport       *string           `json:"transport,omitempty"`
	TimeoutMs       *int              `json:"timeoutMs,omitempty"`
	MaxRetries      *int              `json:"maxRetries,omitempty"`
	MaxRetryDelayMs *int              `json:"maxRetryDelayMs,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Metadata        chord.JsonValue   `json:"metadata,omitempty"`
	CacheRetention  *string           `json:"cacheRetention,omitempty"`
	Deferred        any               `json:"deferred,omitempty"`
}

// HarnessSettings is the harness-wide run policy before resolution.
type HarnessSettings struct {
	// Extensions is the default extension selection; absent means every
	// installed extension, in install order.
	Extensions    []Extension
	Stream        *ConversationStreamOptions
	Retry         *RetryPolicyChange
	Compaction    *CompactionPolicyChange
	ToolExecution *string
	SteeringMode  *string
	FollowUpMode  *string
}

// Settings is the resolved settings: every field over its built-in default.
type Settings struct {
	Extensions    []Extension
	Stream        ConversationStreamOptions
	Retry         ConversationRetryPolicy
	Compaction    CompactionPolicy
	ToolExecution string
	SteeringMode  string
	FollowUpMode  string
}

// ExtensionsSelection is the stored extension choice: an exact list, or an
// edit of the host default selection.
type ExtensionsSelection struct {
	IsList bool
	List   []string
	Add    []string
	Remove []string
}

// MarshalJSON emits the upstream union.
func (s ExtensionsSelection) MarshalJSON() ([]byte, error) {
	if s.IsList {
		return marshalJSON(normalizeStrings(s.List))
	}
	object := map[string]any{}
	if s.Add != nil {
		object["add"] = normalizeStrings(s.Add)
	}
	if s.Remove != nil {
		object["remove"] = normalizeStrings(s.Remove)
	}
	return marshalJSON(object)
}

// UnmarshalJSON decodes the upstream union.
func (s *ExtensionsSelection) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		s.IsList = true
		s.List = list
		return nil
	}
	var object struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	s.IsList = false
	s.List = nil
	s.Add = object.Add
	s.Remove = object.Remove
	return nil
}

// ToolsSelection is the stored tool filter: an exact list, or a removal set.
type ToolsSelection struct {
	IsList bool
	List   []string
	Remove []string
}

// MarshalJSON emits the upstream union.
func (s ToolsSelection) MarshalJSON() ([]byte, error) {
	if s.IsList {
		return marshalJSON(normalizeStrings(s.List))
	}
	return marshalJSON(map[string]any{"remove": normalizeStrings(s.Remove)})
}

// UnmarshalJSON decodes the upstream union.
func (s *ToolsSelection) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		s.IsList = true
		s.List = list
		return nil
	}
	var object struct {
		Remove []string `json:"remove"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	s.IsList = false
	s.List = nil
	s.Remove = object.Remove
	return nil
}

// AgentState is the stored agent choice of one conversation.
type AgentState struct {
	Model         *ModelRef            `json:"model,omitempty"`
	ThinkingLevel *string              `json:"thinkingLevel,omitempty"`
	Extensions    *ExtensionsSelection `json:"extensions,omitempty"`
	Tools         *ToolsSelection      `json:"tools,omitempty"`
	Instructions  *string              `json:"instructions,omitempty"`
	Cwd           *string              `json:"cwd,omitempty"`
}

// AgentChange is a change to `pi.agent`: a given field replaces, null clears,
// absent does nothing.
type AgentChange struct {
	Model         Optional[ModelRef]
	ThinkingLevel Optional[string]
	Extensions    Optional[ExtensionsChange]
	Tools         Optional[ToolsChange]
	Instructions  Optional[string]
	Cwd           Optional[string]
}

// ExtensionsChange is an `AgentChange.extensions` value.
type ExtensionsChange struct {
	IsList bool
	List   []Extension
	Add    []Extension
	Remove []Extension
}

// ToolsChange is an `AgentChange.tools` value.
type ToolsChange struct {
	IsList bool
	List   []ToolRegistration
	Remove []ToolRegistration
}

// Agent is a conversation's agent resolved against a registry snapshot and the
// settings.
type Agent struct {
	Model         *ModelRef
	ThinkingLevel string
	Extensions    []Extension
	Tools         []ToolRegistration
	Sections      []PromptSection
	Instructions  *string
	Cwd           *string
}

// PromptInput is the input to system prompt section rendering.
type PromptInput struct {
	ConversationID Id
	Agent          Agent
	Env            ExecutionEnv
	Shown          map[string]string
	Read           DocumentReader
}

// PromptSection is one system prompt section.
type PromptSection struct {
	Key string
	// Render returns the section text and whether it is present.
	Render func(input PromptInput, ctx chord.Context) (string, bool, error)
	// Tag wraps the text as `<key>…</key>`; absent means true.
	Tag *bool
}

// HookRegistration matches tasks by name.
type HookRegistration struct {
	Task     string
	Handlers any
}

// Wrap targets a tool name or a section key.
type Wrap struct {
	Tool        string
	Section     string
	WrapTool    func(ToolRegistration) ToolRegistration
	WrapSection func(PromptSection) PromptSection
}

// Extension is a named bundle of code selected by conversations by name.
type Extension struct {
	Name     string
	Tools    []ToolRegistration
	Sections []PromptSection
	Hooks    []HookRegistration
	Wraps    []Wrap
	Tasks    []Task
}

// RegistryTool is one installed tool with its extension.
type RegistryTool struct {
	Extension Extension
	Tool      ToolRegistration
}

// RegistrySection is one installed section with its extension.
type RegistrySection struct {
	Extension Extension
	Section   PromptSection
}

// RegistrySnapshot is an immutable view of one published registry state.
type RegistrySnapshot interface {
	Installed() []Extension
	Extension(name string) *Extension
	Tools() []RegistryTool
	Sections() []RegistrySection
	Tasks() []Task
	Task(name string) *Task
}

// RegistryReader is the read side of a registry consumed by a Harness.
type RegistryReader interface {
	Snapshot() RegistrySnapshot
	Subscribe(listener func()) func()
}

// Registry is the application-owned registry of extensions.
type Registry interface {
	RegistryReader
	Install(extension Extension) error
	Uninstall(extension Extension) error
}

// ToolControl is the post-tools control a tool result may request.
type ToolControl struct {
	AddTools  []string
	Terminate bool
	Handoff   *string
}

// ToolExecutionResult is one tool execution's result.
type ToolExecutionResult struct {
	// Content overrides the retained output text.
	Content []ai.UserContent
	IsError *bool
	// Details overrides the last `details()` value.
	Details     chord.JsonValue
	Diagnostics []ToolDiagnostic
	Usage       *ai.Usage
	Control     *ToolControl
}

// ToolRegistration is an executable tool registered in a registry.
type ToolRegistration struct {
	ai.Tool
	// Replay is "safe" or "unsafe" (default).
	Replay string
	// ExecutionMode defaults to the settings' toolExecution.
	ExecutionMode    string
	PrepareArguments func(args chord.JsonValue) (chord.JsonValue, error)
	OutputLimits     *OutputLimits
	Execute          func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error)
}

// SettledTask is a terminal task record.
type SettledTask struct {
	TaskRecord
}

// CompactionResult is the outcome of a compaction task.
type CompactionResult struct {
	EntryID      *Id `json:"entryId,omitempty"`
	SubmissionID *Id `json:"submissionId,omitempty"`
}

// ConversationCreateOptions are the options of a conversation creation.
type ConversationCreateOptions struct {
	Ownership ConversationOwnership
	// Agent is applied in the creating commit after the creation hook's copy,
	// before Init.
	Agent *AgentChange
	Init  func(tx *Transaction, conversationID Id) error
}

// ConversationContextOptions configures a committed context read.
type ConversationContextOptions struct {
	// At limits context to the visible history through this entry.
	At *Id
}

// ConversationAbortOptions configures a conversation abort.
type ConversationAbortOptions struct {
	Background bool
}

// ConversationHandle is the invocation-bound conversation operation set.
type ConversationHandle interface {
	ConversationID() Id
	Submit(draft InputSubmissionDraft, ctx chord.Context) (Submission, error)
	Abort(ctx chord.Context, options *ConversationAbortOptions) error
	WaitForIdle(ctx chord.Context) error
}

// InputSubmissionDraft is user input admitted to a conversation.
type InputSubmissionDraft struct {
	RequestID *string
	Content   UserInput
	WhenBusy  *string
}

// Submission is the awaitable host object for one admitted submission.
type Submission interface {
	ID() Id
	Status(ctx chord.Context) (SubmissionRecord, error)
	Wait(ctx chord.Context) (SubmissionRecord, error)
	Abort(ctx chord.Context) (string, error)
}

// DocumentReader is the committed (non-watching) document read side.
type DocumentReader interface {
	Snapshot(ctx chord.Context, definition DocDefinition, args ...any) (chord.JsonValue, bool, error)
	SnapshotAsOf(ctx chord.Context, definition DocDefinition, conversationID Id, at Id) (chord.JsonValue, bool, error)
}

// DocumentObserver is the non-creating document watch side.
type DocumentObserver interface {
	WatchDoc(ctx chord.Context, definition DocDefinition, args ...any) (DocumentWatch[chord.JsonValue], bool, error)
}

// ToolExecutionApi is the operation set available to one tool invocation.
// Generic upstream methods are erased to `any` in this port.
type ToolExecutionApi interface {
	DocumentObserver
	DocumentReader
	TaskID() Id
	ConversationID() Id
	CallID() string
	Registry() RegistrySnapshot
	Agent(ctx chord.Context) (Agent, error)
	// Models is the HarnessOptions model catalog shared with generation.
	Models() *ai.Models
	Env() ExecutionEnv
	Output(chunk []byte)
	Diagnostic(diagnostic ToolDiagnostic)
	Details(value chord.JsonValue, ctx chord.Context) error
	Commit(change func(tx *Transaction) (any, error), ctx chord.Context) (any, error)
	Memo(ctx chord.Context, name string) (chord.JsonValue, bool, error)
	MemoSet(ctx chord.Context, name string, candidate chord.JsonValue) (chord.JsonValue, error)
	CreateTask(task Task, input chord.JsonValue, options TaskOptions, ctx chord.Context) (Id, error)
	GetTask(id Id, ctx chord.Context) (*TaskRecord, error)
	WaitForTask(id Id, ctx chord.Context) (SettledTask, error)
	Conversation(id Id, ctx chord.Context) (ConversationHandle, bool, error)
}

// normalizeStrings keeps a non-nil empty slice as an empty array in JSON.
func normalizeStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
