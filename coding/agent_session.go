package coding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Port of core/agent-session.ts — the AgentSession facade. The extension
// runner, extension UI modes, and extension commands are out of scope by
// design (the user extends the repo directly); the retained surface is:
// session persistence from agent events, steering/follow-up queue display
// tracking, auto-compaction on threshold/overflow, auto-retry on transient
// errors, stats, and event fan-out.

// Defaults (core/defaults.ts).
const (
	DefaultThinkingLevel = ai.ThinkMedium
)

// ThinkingLevelOptions is the full level list.
var ThinkingLevelOptions = []ai.ThinkingLevel{
	ai.ThinkOff, ai.ThinkMinimal, ai.ThinkLow, ai.ThinkMedium,
	ai.ThinkHigh, ai.ThinkXHigh, ai.ThinkMax,
}

// ParsedSkillBlock is a parsed <skill> block from message text.
type ParsedSkillBlock struct {
	Name        string
	Location    string
	Content     string
	UserMessage string
}

var skillBlockPattern = regexp.MustCompile(
	`^<skill name="([^"]+)" location="([^"]+)">\n([\s\S]*?)\n</skill>(?:\n\n([\s\S]+))?$`)

// ParseSkillBlock parses a skill block from message text; nil when absent.
func ParseSkillBlock(text string) *ParsedSkillBlock {
	match := skillBlockPattern.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	return &ParsedSkillBlock{
		Name: match[1], Location: match[2], Content: match[3],
		UserMessage: strings.TrimSpace(match[4]),
	}
}

// CompactionReason discriminates compaction triggers.
type CompactionReason = string

const (
	CompactionManual    CompactionReason = "manual"
	CompactionThreshold CompactionReason = "threshold"
	CompactionOverflow  CompactionReason = "overflow"
)

// SessionEventType names session-level events.
type SessionEventType = string

const (
	SessionAgentStart                     SessionEventType = "agent_start"
	SessionAgentEnd                       SessionEventType = "agent_end"
	SessionTurnStart                      SessionEventType = "turn_start"
	SessionTurnEnd                        SessionEventType = "turn_end"
	SessionMessageStart                   SessionEventType = "message_start"
	SessionMessageUpdate                  SessionEventType = "message_update"
	SessionMessageEnd                     SessionEventType = "message_end"
	SessionToolExecutionStart             SessionEventType = "tool_execution_start"
	SessionToolExecutionUpdate            SessionEventType = "tool_execution_update"
	SessionToolExecutionEnd               SessionEventType = "tool_execution_end"
	SessionQueueUpdate                    SessionEventType = "queue_update"
	SessionCompactionStart                SessionEventType = "compaction_start"
	SessionCompactionEnd                  SessionEventType = "compaction_end"
	SessionInfoChanged                    SessionEventType = "session_info_changed"
	SessionThinkingLevelChanged           SessionEventType = "thinking_level_changed"
	SessionAutoRetryStart                 SessionEventType = "auto_retry_start"
	SessionAutoRetryEnd                   SessionEventType = "auto_retry_end"
	SessionBashExecutionUpdate            SessionEventType = "bash_execution_update"
	SessionEntryAppended                  SessionEventType = "entry_appended"
	SessionSummarizationRetryScheduled    SessionEventType = "summarization_retry_scheduled"
	SessionSummarizationRetryAttemptStart SessionEventType = "summarization_retry_attempt_start"
	SessionSummarizationRetryFinished     SessionEventType = "summarization_retry_finished"
	SessionAgentSettled                   SessionEventType = "agent_settled"
)

// SessionEvent extends the core agent events with session-level payloads.
type SessionEvent struct {
	Type SessionEventType
	// Agent carries the underlying event for passthrough types.
	Agent *agent.AgentEvent
	// agent_end / compaction_end
	WillRetry bool
	// agent_settled: an armed goal is still active, so another round follows
	// (the interactive working period stays open, D402). Not serialized.
	GoalPursuing bool
	// agent_settled: follow-ups this run held (D385) are released as a run of
	// their own after this settle, so the working period stays open for them
	// too (D402). Not serialized.
	FollowUpsPending bool
	// queue_update
	Steering []string
	FollowUp []string
	// compaction_start/end
	Reason       CompactionReason
	Result       *CompactionResult
	Aborted      bool
	ErrorMessage string
	// session_info_changed (nil represents upstream undefined, including clears)
	Name *string
	// thinking_level_changed
	Level ai.ThinkingLevel
	// auto_retry_*
	Attempt     int
	MaxAttempts int
	DelayMS     int64
	Success     bool
	// bash_execution_update
	ID    string
	Delta string
	// entry_appended
	Entry *SessionEntry
	// summarization_retry_attempt_start
	Source string // "branchSummary" | "compaction"
}

// SessionEventListener receives session events.
type SessionEventListener func(event *SessionEvent)

// SessionSettings bundle the tuning knobs.
type SessionSettings struct {
	Retry      *ai.RetryPolicy
	Compaction CompactionSettings
}

// AgentSession wires the stateful Agent, the SessionManager, and the
// built-in tool set into one session with persistence, auto-compaction,
// and auto-retry.
type AgentSession struct {
	Agent    *agent.Agent
	Sessions *SessionManager
	Settings SessionSettings
	Cwd      string

	// mu guards retry state, queue-display tracking and the control block.
	mu sync.Mutex

	// control holds the optional collaborators and mutation state ported from
	// the upstream session's state/control surface.
	control *AgentSessionControl

	// CompactionStreamFn is the stream function used for compaction and branch
	// summaries (upstream uses the agent's stream function).
	CompactionStreamFn StreamFnFn

	// promptState buffers custom and next-turn messages across a run.
	promptState *promptState

	// CacheWarmer keeps the prompt cache entry of session requests warm.
	CacheWarmer *CacheWarmer

	// goalHeldThisRun records that a goal was active during the current run, so
	// its user follow-ups stay queued until the run's final answer even when the
	// goal completes mid-run.
	goalHeldThisRun atomic.Bool

	// followUpReleasePending records that a run carrying released follow-ups is
	// queued or waiting to start, so the settle that precedes it reports the
	// follow-up run that is still coming (D402).
	followUpReleasePending atomic.Bool

	// Goal is the builtin session-goal controller (nil when disabled).
	Goal *GoalController

	// requestPrefix remembers the last request the agent sent, so compaction can
	// summarize from it instead of reserializing the conversation (D419).
	requestPrefix *requestPrefixCapture
	// convertToLlm is the config's converter, so a compaction can tell whether
	// the current context still starts with the captured request (D419).
	convertToLlm func([]ai.Message) []ai.Message

	// streamFn is the session's model stream function (compaction, summaries).
	streamFn agent.StreamFn
	// streamFnResolvesAuth mirrors SessionConfig.StreamFnResolvesAuth.
	streamFnResolvesAuth bool

	// unsubscribeAgent detaches handleAgentEvent; Dispose calls it
	// (upstream _disconnectFromAgent).
	unsubscribeAgent func()

	listenerMu sync.Mutex
	listeners  []*sessionListenerKey

	steeringMessages []string
	followUpMessages []string

	lastAssistantMessage *ai.AssistantMessage
	// entryIDsByMessage maps an appended agent message to its session entry id so
	// post-run recovery can persist a context_edit omission for it (upstream
	// _entryIdsByMessage / _findPersistedMessageEntryId).
	entryIDsByMessage map[ai.Message]string
	retryAttempt      int
	retryCancel       context.CancelFunc
	retryActive       bool
	willRetry         bool

	compactionCancel context.CancelFunc
	overflowRecovery overflowRecoveryState

	bashMu              sync.Mutex
	bashNextID          int
	bashCancels         map[int]context.CancelFunc
	pendingBashMessages []*ai.CustomMessage

	// System prompt options for section diffing on tool changes.
	//
	// promptOptionsMu guards the field across goroutines: the work goroutine
	// re-applies the loadout between turns, while the UI loop rebuilds the
	// options on /reload and tool changes.
	SystemPromptOptions *BuildSystemPromptOptions
	promptOptionsMu     sync.Mutex
	// toolsMu serializes every read-modify-write of the active tool set
	// (SetActiveToolsByName, AttachExtraTools, the goal's tool sync). Each reads
	// the active names and sets a new list; overlapping, the later set dropped
	// what the other had just activated. Taken before stateMu and the agent's
	// lock, and held over no callbacks.
	toolsMu sync.Mutex

	// skillDiagnostics are the skill loader's warnings/collisions. They,
	// promptDiagnostics and control.PromptTemplates are replaced by Reload and
	// guarded by promptOptionsMu.
	skillDiagnostics  []ResourceDiagnostic
	promptDiagnostics []ResourceDiagnostic

	// resources are the CLI-derived resource switches (--skill and friends),
	// kept so a reload re-reads with the same options instead of resetting them.
	resources resourceOptions

	// agentDir and promptSources are the inputs Reload re-reads the resource
	// files from (upstream ResourceLoader's config; its extension side is out
	// of scope, D41/D140).
	agentDir      string
	promptSources PromptFileSources

	// usesDefaultTools, allowedToolNames and excludedToolNames carry the
	// tool-selection inputs Reload and late tool registration need (upstream
	// _usesDefaultTools, _allowedTools and _excludedTools).
	usesDefaultTools     bool
	defaultToolModifiers []string
	allowedToolNames     []string
	excludedToolNames    []string

	// beforeFirstTurn runs once before the first agent turn (upstream
	// before_agent_start; the MCP first-prompt wait installs here).
	beforeFirstTurn func(ctx context.Context)
	firstTurnOnce   sync.Once
	firstTurnRan    bool
}

type sessionListenerKey struct {
	fn SessionEventListener
}

// SessionConfig configures NewAgentSession.
type SessionConfig struct {
	Cwd      string
	Model    *ai.Model
	StreamFn agent.StreamFn
	// StreamFnResolvesAuth marks a stream function that resolves provider auth
	// through the session's own ModelRuntime (the SDK's). Summaries must then
	// not pre-resolve credentials (pi v1.5.0 6fed3cfb9).
	StreamFnResolvesAuth bool
	APIKey               string
	SystemPrompt         string
	// AppendSystemPrompt is appended to the prompt (before project context).
	AppendSystemPrompt string
	// PromptSourcePaths are the loaded system/append prompt files.
	PromptSourcePaths []string
	// AgentDir is the global agent directory: the resource files are re-read
	// from it on Reload.
	AgentDir string
	// PromptSources are the explicit system/append prompt inputs (CLI text or
	// paths); Reload re-resolves them against the current trust state and
	// re-reads the discovered files when they are nil.
	PromptSources *PromptFileSources
	// Control holds the session's collaborators (model runtime, settings
	// manager, tool registry, toggles). NewAgentSession always installs one:
	// the config's block when present, otherwise an empty block. A session is
	// never half-built, so its methods do not guard against a missing block;
	// the fields inside it stay optional.
	Control       *AgentSessionControl
	Tools         []agent.AgentTool
	Sessions      *SessionManager
	Settings      SessionSettings
	ThinkingLevel ai.ThinkingLevel
	Skills        []Skill
	ContextFiles  []ContextFile
	// SkillDiagnostics are the skill loader's warnings/collisions, surfaced in
	// the interactive loaded-resources area.
	SkillDiagnostics  []ResourceDiagnostic
	PromptDiagnostics []ResourceDiagnostic
	InitialMessages   []ai.Message
	// ConvertToLlm transforms messages before provider calls (the block-images
	// filter).
	ConvertToLlm func(messages []ai.Message) []ai.Message
	// SessionID is forwarded to providers for cache-aware backends.
	SessionID string
	// UsesDefaultTools records that the initial tool selection came from the
	// `defaultTools` setting (or its built-in fallback) rather than explicit
	// options: on Reload, tools newly added to the setting are activated
	// (upstream AgentSessionConfig.usesDefaultTools, db6cc71dc).
	UsesDefaultTools bool
	// DefaultToolModifiers are modifier-only `--tools` entries applied to
	// settings-derived defaults on creation and reload (upstream ddaa0a034).
	DefaultToolModifiers []string
	// ExcludedToolNames are the CLI exclusions applied at creation and again on
	// Reload when the setting adds tools (upstream _excludedToolNames).
	ExcludedToolNames []string
	// AllowedToolNames is a non-nil explicit tool allowlist. A non-nil empty
	// list disables late-registered tools, as with --no-tools.
	AllowedToolNames []string
	// Agent-level request wiring from settings.
	SteeringMode    QueueMode
	FollowUpMode    QueueMode
	Transport       ai.Transport
	ThinkingBudgets *ai.ThinkingBudgets
	MaxRetryDelayMS *int
	// BeforeFirstTurn runs once before the first agent turn starts (upstream
	// before_agent_start, where the MCP extension awaits the direct-tool
	// servers, bounded). The caller owns the wait; long work here delays the
	// first request by design.
	BeforeFirstTurn func(ctx context.Context)
	// PrepareNextTurnWithContext runs between turns, before the session's own
	// refresh (upstream agent-session.ts chains the hook the Agent already
	// carried). Its messages are emitted before the loadout update, its context
	// replaces the turn context, and the session's state still supplies the
	// model, thinking level and tools.
	PrepareNextTurnWithContext func(turn *agent.AgentTurnContext, ctx context.Context) (*agent.AgentLoopTurnUpdate, error)
	// Goal enables the builtin session-goal controller. The SDK sets it unless
	// tools are disabled; upstream always has the extension host available.
	Goal bool
}

// SetBeforeFirstTurn installs the once-per-session pre-turn hook (the
// interactive mode wires the MCP first-prompt wait after session creation;
// a turn that already ran never sees it).
func (s *AgentSession) SetBeforeFirstTurn(hook func(ctx context.Context)) {
	s.mu.Lock()
	s.beforeFirstTurn = hook
	s.mu.Unlock()
}

// HasRunFirstTurn reports whether a turn already ran (the once-per-session
// pre-turn hook has fired or been skipped; a later install would never run).
func (s *AgentSession) HasRunFirstTurn() bool {
	s.mu.Lock()
	ran := s.firstTurnRan
	s.mu.Unlock()
	return ran
}

// NewAgentSession builds the wired session.
func NewAgentSession(config *SessionConfig) (*AgentSession, error) {
	if config == nil {
		return nil, fmt.Errorf("config required")
	}
	settings := config.Settings
	if settings.Compaction.ReserveTokens == 0 {
		settings.Compaction = DefaultCompactionSettings
	}

	sessionManager := config.Sessions
	if sessionManager == nil {
		sessionManager = NewSessionManager(config.Cwd, nil)
	}

	initialState := &agent.AgentInitialState{
		// The base prompt rides in the system prompt's preamble section, which
		// preparePromptAndToolLoadout installs (upstream buildSystemPromptSections
		// reads customPrompt into the preamble, not into message content).
		SystemPrompt:  "",
		Model:         config.Model,
		ThinkingLevel: config.ThinkingLevel,
		Tools:         config.Tools,
		Messages:      config.InitialMessages,
	}
	// Capture the request the agent sends, byte for byte, before it leaves: a
	// rebuilt one can drift (tool loadout, prompt sections, thinking level) and
	// a drifted prefix costs the cache hit it exists for.
	requestPrefix := &requestPrefixCapture{}
	agentStreamFn := config.StreamFn
	if agentStreamFn != nil {
		inner := agentStreamFn
		agentStreamFn = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			requestPrefix.record(model, transcript, options)
			return inner(model, transcript, options)
		}
	}
	a, err := agent.NewAgent(&agent.AgentOptions{
		InitialState:    initialState,
		StreamFn:        agentStreamFn,
		ConvertToLlm:    config.ConvertToLlm,
		SessionID:       config.SessionID,
		SteeringMode:    config.SteeringMode,
		FollowUpMode:    config.FollowUpMode,
		Transport:       config.Transport,
		ThinkingBudgets: config.ThinkingBudgets,
		MaxRetryDelayMS: config.MaxRetryDelayMS,
		// Installed here only so the session's own refresh can chain it below;
		// assigning it directly would drop that refresh.
		PrepareNextTurnWithContext: config.PrepareNextTurnWithContext,
	})
	if err != nil {
		return nil, err
	}

	// The block is taken by pointer (it holds a mutex); a session without one
	// gets an empty block so it is never half-built.
	control := config.Control
	if control == nil {
		control = &AgentSessionControl{}
	}
	if control.Tools == nil {
		control.Tools = map[string]AgentToolDefinition{}
	}
	// Seed the registry from the configured tools so preparePromptAndToolLoadout
	// (and SetActiveToolsByName) resolve them even when the caller did not build
	// a full registry (the SDK replaces these with its own definitions).
	for _, tool := range config.Tools {
		if _, ok := control.Tools[tool.Name]; !ok {
			control.Tools[tool.Name] = AgentToolDefinition{Tool: tool}
		}
	}

	var copiedAllowedToolNames []string
	if config.AllowedToolNames != nil {
		copiedAllowedToolNames = append([]string{}, config.AllowedToolNames...)
	}
	s := &AgentSession{
		Agent:                a,
		Sessions:             sessionManager,
		Settings:             settings,
		Cwd:                  config.Cwd,
		control:              control,
		requestPrefix:        requestPrefix,
		convertToLlm:         llmConverter(config.ConvertToLlm),
		streamFn:             config.StreamFn,
		streamFnResolvesAuth: config.StreamFnResolvesAuth,
		SystemPromptOptions: &BuildSystemPromptOptions{
			CustomPrompt: config.SystemPrompt, AppendSystemPrompt: config.AppendSystemPrompt, Cwd: config.Cwd,
			Skills: config.Skills, ContextFiles: config.ContextFiles,
			PromptSourcePaths: append([]string{}, config.PromptSourcePaths...),
			SelectedTools:     agentToolNames(config.Tools),
		},
		skillDiagnostics:     config.SkillDiagnostics,
		promptDiagnostics:    config.PromptDiagnostics,
		agentDir:             config.AgentDir,
		usesDefaultTools:     config.UsesDefaultTools,
		defaultToolModifiers: append([]string{}, config.DefaultToolModifiers...),
		allowedToolNames:     copiedAllowedToolNames,
		excludedToolNames:    excludedToolNameSet(config.ExcludedToolNames),
		beforeFirstTurn:      config.BeforeFirstTurn,
	}
	// A default-tools session restores the loadout its transcript records, once
	// the registry holds it, with modifier-only --tools entries applied
	// (upstream 6f2f164ab: the constructor restored only when the caller passed
	// no initial tools, which the SDK never does).
	if s.usesDefaultTools {
		s.RestoreToolsFromTranscript(s.defaultToolModifiers...)
	}
	// A fresh session must carry its initial tool declaration in the transcript.
	// The agent seeds the declaration into state but never emits it, so the
	// persisted transcript would otherwise hold only the deltas (goal/MCP tools)
	// recorded later. A resume restores the loadout from the transcript, so
	// without this first entry a default-tools session resolves no tools (or
	// only the later additions) — the reference has the same defect (D237).
	// Append it once, only for a session with no prior entries and no
	// caller-supplied initial messages.
	if len(config.InitialMessages) == 0 && !sessionManager.hasEntries() {
		if messages := s.Agent.State().Messages; len(messages) > 0 {
			if first, ok := messages[0].(*ai.SystemMessage); ok && len(first.ToolsAdded) > 0 {
				sessionManager.AppendMessage(first)
			}
		}
	}
	if config.PromptSources != nil {
		s.promptSources = *config.PromptSources
	}
	// The builtin goal controller is a session feature: register its tools and
	// replay the durable goal state from the current branch. The controller is
	// bound to this session, so its effects reach it directly.
	if config.Goal {
		s.Goal = newGoalController(nil, newSessionGoalSink(s))
		s.registerGoalTools()
		s.Goal.Start("startup")
	}
	// An active goal holds user follow-ups to the end of its run.
	a.DeferFollowUp = s.deferGoalFollowUp
	if s.Goal != nil {
		s.Goal.SetHoldFollowUps(s.goalHoldsFollowUps)
	}
	// Always subscribed: session persistence, queue tracking, compaction,
	// retry logic.
	s.unsubscribeAgent = a.Subscribe(func(event agent.AgentEvent, ctx context.Context) error {
		s.handleAgentEvent(&event)
		return nil
	})

	// Auto-compaction runs before the next assistant response, mirroring
	// upstream's _compactBeforeNextAssistantResponse; the refreshed message
	// list replaces the turn context. The agent state is also re-read here so a
	// model or thinking-level switch made while the turn is running reaches the
	// next assistant request in that same turn, not only the next prompt
	// (upstream agent-session.ts prepareNextTurnWithContext, applied by the loop
	// as nextTurnSnapshot). The prompt/tool loadout is re-applied too, so a
	// changed system prompt or tool set reaches the rest of the turn.
	//
	// The caller's hook is chained, as upstream's _installAgentNextTurnRefresh
	// chains the hook the Agent already carried: it runs after compaction, its
	// messages precede the loadout update, its context replaces the turn
	// context, and the session's state keeps supplying model, thinking level and
	// tools.
	previousNextTurn := config.PrepareNextTurnWithContext
	a.PrepareNextTurnWithContext = func(turn *agent.ShouldStopAfterTurnContext, ctx context.Context) (*agent.AgentLoopTurnUpdate, error) {
		if err := s.maybeAutoCompact(ctx); err != nil {
			return nil, err
		}
		var caller *agent.AgentLoopTurnUpdate
		if previousNextTurn != nil {
			update, err := previousNextTurn(turn, ctx)
			if err != nil {
				return nil, err
			}
			caller = update
		}
		var prepared []ai.Message
		if caller != nil {
			prepared = append(prepared, caller.Messages...)
		}
		if update := s.preparePromptAndToolLoadout(); update != nil {
			prepared = append(prepared, update)
		}
		state := s.Agent.State()
		if s.Goal != nil {
			if reminder := s.Goal.roundPromptIfMissing(state.Messages); reminder != nil {
				prepared = append(prepared, reminder)
			}
		}
		updated := turn.Context
		if caller != nil && caller.Context != nil {
			updated = *caller.Context
		} else {
			updated.Messages = state.Messages
		}
		updated.Tools = append([]agent.AgentTool{}, state.Tools...)
		return &agent.AgentLoopTurnUpdate{
			Context:          &updated,
			Messages:         prepared,
			Model:            state.Model,
			ThinkingLevel:    state.ThinkingLevel,
			HasThinkingLevel: true,
		}, nil
	}
	return s, nil
}

// Subscribe adds a session listener; the returned func unsubscribes.
func (s *AgentSession) Subscribe(listener SessionEventListener) func() {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	key := &sessionListenerKey{fn: listener}
	s.listeners = append(s.listeners, key)
	return func() {
		s.listenerMu.Lock()
		defer s.listenerMu.Unlock()
		for i, k := range s.listeners {
			if k == key {
				s.listeners = slices.Delete(s.listeners, i, i+1)
				return
			}
		}
	}
}

func (s *AgentSession) emit(event *SessionEvent) {
	s.listenerMu.Lock()
	listeners := append([]*sessionListenerKey{}, s.listeners...)
	s.listenerMu.Unlock()
	for _, key := range listeners {
		key.fn(event)
	}
}

// handleAgentEvent ports the persistence/queue/compaction event handling.
func (s *AgentSession) handleAgentEvent(event *agent.AgentEvent) {
	if s.Goal != nil && s.Goal.Pursuing() {
		s.goalHeldThisRun.Store(true)
	}
	// Queue display tracking: user message starts remove queued entries
	// BEFORE the event fans out. Text is extracted Blocks-aware: queued
	// messages are built with block content (queuedUserMessage), whose Text field is
	// empty — matching on Content.Text alone left the dock banner showing the
	// steered message after it was pushed into the transcript.
	if event.Type == agent.MessageStart && event.Message != nil && ai.RoleOf(event.Message) == ai.RoleUser {
		if user, ok := event.Message.(*ai.UserMessage); ok {
			messageText := contentTextJoinedNoSep(user.Content)
			changed := false
			s.mu.Lock()
			s.overflowRecovery.attempted = false
			if idx := indexOf(s.steeringMessages, messageText); idx != -1 {
				last := len(s.steeringMessages) - 1
				copy(s.steeringMessages[idx:], s.steeringMessages[idx+1:])
				s.steeringMessages[last] = ""
				s.steeringMessages = s.steeringMessages[:last]
				changed = true
			} else if idx := indexOf(s.followUpMessages, messageText); idx != -1 {
				last := len(s.followUpMessages) - 1
				copy(s.followUpMessages[idx:], s.followUpMessages[idx+1:])
				s.followUpMessages[last] = ""
				s.followUpMessages = s.followUpMessages[:last]
				changed = true
			}
			s.mu.Unlock()
			if changed {
				s.emitQueueUpdate()
			}
		}
	}

	// Notify listeners (agent_end carries willRetry).
	if event.Type == agent.AgentEnd {
		s.willRetry = s.willRetryAfterAgentEnd(event)
		// The goal controller's agent_end and agent_before_settle hooks run before
		// listeners. A completed settle may queue the next round; a retryable
		// failure keeps the current round active for runPostRunLoop to retry.
		if s.Goal != nil {
			s.Goal.AgentEnd(event)
		}
		s.emit(&SessionEvent{Type: SessionAgentEnd, Agent: event, WillRetry: s.willRetry})
		s.releaseGoalHeldFollowUps(event)
	} else {
		s.emit(&SessionEvent{Type: event.Type, Agent: event})
	}

	// Session persistence.
	if event.Type == agent.MessageEnd && event.Message != nil {
		switch msg := event.Message.(type) {
		case *ai.AssistantMessage:
			entryID := s.Sessions.AppendMessage(event.Message)
			s.mu.Lock()
			if s.entryIDsByMessage == nil {
				s.entryIDsByMessage = map[ai.Message]string{}
			}
			s.entryIDsByMessage[event.Message] = entryID
			s.lastAssistantMessage = msg
			// A completed response ends the one-attempt overflow recovery window
			// (upstream resets _overflowRecoveryAttempted for any non-error,
			// non-length stop).
			if msg.StopReason != ai.StopError && msg.StopReason != ai.StopLength {
				s.overflowRecovery.attempted = false
			}
			attempt := s.retryAttempt
			if msg.StopReason != ai.StopError && attempt > 0 {
				s.retryAttempt = 0
			}
			s.mu.Unlock()
			if msg.StopReason != ai.StopError && attempt > 0 {
				s.emit(&SessionEvent{Type: SessionAutoRetryEnd, Success: true, Attempt: attempt})
			}
		case *ai.SystemMessage, *ai.UserMessage, *ai.ToolResultMessage:
			entryID := s.Sessions.AppendMessage(event.Message)
			s.mu.Lock()
			if s.entryIDsByMessage == nil {
				s.entryIDsByMessage = map[ai.Message]string{}
			}
			s.entryIDsByMessage[event.Message] = entryID
			s.mu.Unlock()
		case *ai.CustomMessage:
			if msg.Role == RoleCustom {
				var fields customMessageFields
				_ = json.Unmarshal(msg.Content, &fields)
				s.Sessions.AppendCustomMessageEntry(fields.CustomType, fields.Content, fields.Display, fields.Details)
			}
			// bashExecution/compactionSummary/branchSummary persist elsewhere.
		}
	}
	if event.Type == agent.TurnEnd {
		// Context-only custom messages must follow every tool result, but reach
		// the model before the next turn (upstream _handleAgentEvent).
		s.FlushPendingCustomMessages()
	}
}

// goalHoldsFollowUps reports whether the current run was entered with a goal,
// so its user follow-ups wait for the goal's final answer.
func (s *AgentSession) goalHoldsFollowUps() bool { return s.goalHeldThisRun.Load() }

// deferGoalFollowUp is the agent's DeferFollowUp predicate: user messages stay
// queued while the goal holds the run; goal rounds are custom messages and
// still pass.
func (s *AgentSession) deferGoalFollowUp(message ai.Message) bool {
	return s.goalHoldsFollowUps() && ai.RoleOf(message) == ai.RoleUser
}

// releaseGoalHeldFollowUps starts the run the goal held back, once the goal is
// no longer pursuing and the run in which it was active has ended with its
// final answer. An aborted run leaves its queue to the caller (interactive
// restores it to the editor).
func (s *AgentSession) releaseGoalHeldFollowUps(event *agent.AgentEvent) {
	if s.Goal == nil || !s.goalHeldThisRun.Load() || s.Goal.Pursuing() || goalAgentAborted(event) {
		return
	}
	// Release with the queue's own granularity: in one-at-a-time mode the
	// released run carries the first follow-up, and that run's stop boundary
	// delivers the rest one turn at a time (D385).
	messages := s.Agent.ReleaseFollowUpMessages()
	if len(messages) == 0 {
		return
	}
	run := func(ctx context.Context) error {
		// A disposed session must not start the run: the release can be queued
		// while the host is shutting down and disposing the session. The
		// released messages run through the session pipeline, so the run emits
		// agent_settled and keeps retry and compaction handling. The goal chain
		// before it settled as one run (rounds continue inside it).
		return s.runReleasedFollowUps(ctx, messages)
	}
	// The released run starts after this run settles, so the settle reports it:
	// the working period stays open across the release (D402).
	s.followUpReleasePending.Store(true)
	if s.Goal.RunAfterSettle(run) {
		return
	}
	// No host work slot (SDK and print sessions): the current run is still
	// unwinding inside its agent-end callback, so wait for it before starting
	// the released run.
	go func() {
		_ = s.WaitForIdle(context.Background())
		_ = run(context.Background())
	}()
}

// runReleasedFollowUps starts the run for follow-ups the goal released. D438:
// another caller's run can start between the release and this start; the
// follow-ups then go back to the front of the queue for that run to take,
// decided under the run-lifecycle lock like queueWhileRunActive, instead of
// failing "already processing" and dropping them. Their display text was never
// removed, so it still shows them queued.
func (s *AgentSession) runReleasedFollowUps(ctx context.Context, messages []ai.Message) error {
	for {
		if s.requeueFollowUpsWhileRunActive(messages) {
			return nil
		}
		err := s.runAgentPromptUnlessDisposed(ctx, messages)
		if !errors.Is(err, errRunAlreadyActive) {
			return err
		}
	}
}

// requeueFollowUpsWhileRunActive puts the follow-ups back at the front of the
// queue and reports true while a run is active; otherwise it reports false.
func (s *AgentSession) requeueFollowUpsWhileRunActive(messages []ai.Message) bool {
	state := s.prompt()
	state.mu.Lock()
	active := state.runActive || s.Agent.State().IsStreaming
	if active {
		s.Agent.RequeueFollowUpMessages(messages)
	}
	state.mu.Unlock()
	return active
}

func indexOf(list []string, want string) int { return slices.Index(list, want) }

func (s *AgentSession) emitQueueUpdate() {
	s.mu.Lock()
	event := &SessionEvent{
		Type:     SessionQueueUpdate,
		Steering: append([]string{}, s.steeringMessages...),
		FollowUp: append([]string{}, s.followUpMessages...),
	}
	s.mu.Unlock()
	s.emit(event)
}

// willRetryAfterAgentEnd: retry policy enabled, attempts remain, and the last
// assistant error is retryable (context overflow is not).
func (s *AgentSession) willRetryAfterAgentEnd(event *agent.AgentEvent) bool {
	settings := s.retrySettings()
	if settings == nil || !settings.Enabled {
		return false
	}
	s.mu.Lock()
	attempt := s.retryAttempt
	s.mu.Unlock()
	if attempt >= settings.MaxRetries {
		return false
	}
	for _, v := range slices.Backward(event.Messages) {
		if assistant, ok := v.(*ai.AssistantMessage); ok {
			return s.IsRetryableError(assistant)
		}
	}
	return false
}

// Steer queues a steering message (drained after the current turn). The queue
// records the message's text so it can be restored to the editor.
func (s *AgentSession) Steer(message ai.Message) {
	s.enqueue(message, false)
	s.emitQueueUpdate()
}

// FollowUp queues a message to run after the agent would stop.
func (s *AgentSession) FollowUp(message ai.Message) {
	s.enqueue(message, true)
	s.emitQueueUpdate()
}

// enqueue adds the message to the agent's steering or follow-up queue and
// records its text for the queue display, in one step under s.mu, so a run
// that takes the message cannot do so before its text is recorded. It emits
// nothing; the caller emits the queue update after releasing its locks.
func (s *AgentSession) enqueue(message ai.Message, followUp bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, isUser := message.(*ai.UserMessage)
	if followUp {
		s.Agent.FollowUp(message)
		if isUser {
			s.followUpMessages = append(s.followUpMessages, contentTextJoinedNoSep(user.Content))
		}
		return
	}
	s.Agent.Steer(message)
	if isUser {
		s.steeringMessages = append(s.steeringMessages, contentTextJoinedNoSep(user.Content))
	}
}

// queueWhileRunActive queues the message for the active run and reports true,
// or reports false without queueing when no run is active, so the caller
// starts one. D438: the check and the queueing are one step under the
// run-lifecycle lock, and the run's end checks for queued messages under the
// same lock (endRun), so a queued message is always either taken by the run
// or finds the session idle. Deciding on an unlocked IsStreaming and queueing
// afterwards could leave the message queued after the run had ended.
func (s *AgentSession) queueWhileRunActive(message ai.Message, followUp bool) bool {
	state := s.prompt()
	state.mu.Lock()
	active := state.runActive || s.Agent.State().IsStreaming
	if active {
		s.enqueue(message, followUp)
	}
	state.mu.Unlock()
	if active {
		s.emitQueueUpdate()
	}
	return active
}

// PromptText runs a prompt from text.
func (s *AgentSession) PromptText(ctx context.Context, input string) error {
	// The canonical pipeline (upstream prompt): expansion, validation,
	// compaction checks and the prompt/tool loadout. Upstream has no separate
	// promptText; print mode calls prompt too.
	return s.Prompt(ctx, input, nil)
}

// PromptMessages runs a prompt from messages.
func (s *AgentSession) PromptMessages(ctx context.Context, messages []ai.Message) error {
	return s.Agent.PromptMessages(ctx, messages)
}

// WaitForIdle waits for the current run to settle.
func (s *AgentSession) WaitForIdle(ctx context.Context) error {
	if err := s.Agent.WaitForIdle(ctx); err != nil {
		return err
	}
	return s.waitForSessionIdle(ctx)
}

// ---------------------------------------------------------------------------
// Compaction
// ---------------------------------------------------------------------------

// CompactSession runs a manual compaction over the current leaf path
// (upstream compact(customInstructions?)): aborts any active run, emits the
// compaction events, resolves summarization auth, and distinguishes
// "Already compacted" from "Nothing to compact (session too small)".
func (s *AgentSession) CompactSession(ctx context.Context, customInstructions string) (*CompactionResult, error) {
	// Abort cancels any running compaction and waits for the session to go
	// idle, so a second manual compaction cannot claim its cancel while the
	// first is live: it aborts it and takes over (upstream compact()). Keep the
	// order: claiming before Abort would let Abort cancel this very compaction.
	s.Abort(ctx)

	compactionCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.compactionCancel = cancel
	s.mu.Unlock()

	s.emit(&SessionEvent{Type: SessionCompactionStart, Reason: CompactionManual})

	// Upstream routes every compaction error through the catch that emits
	// compaction_end; skipping the end event left the compaction status
	// indicator mounted forever. Clear the state before notifying so end
	// listeners observe an idle session and may submit queued prompts.
	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		s.mu.Lock()
		s.compactionCancel = nil
		s.mu.Unlock()
		cancel()
		s.resolveIdleWaitIfIdle()
	}
	defer cleanup()

	result, err := s.runManualCompaction(compactionCtx, customInstructions)
	// Aborted means the compaction context was cancelled (ESC/user abort);
	// compute it before cleanup's own cancel() runs.
	aborted := compactionCtx.Err() != nil
	cleanup()
	if err != nil {
		errorMessage := ""
		if !aborted {
			errorMessage = "Compaction failed: " + err.Error()
		}
		s.emit(&SessionEvent{
			Type: SessionCompactionEnd, Reason: CompactionManual, Aborted: aborted,
			ErrorMessage: errorMessage,
		})
		return nil, err
	}
	s.emit(&SessionEvent{Type: SessionCompactionEnd, Reason: CompactionManual, Result: result})
	return result, nil
}

// runManualCompaction runs the manual compaction after compaction_start was
// emitted. All errors are reported through compaction_end by the caller.
func (s *AgentSession) runManualCompaction(ctx context.Context, customInstructions string) (*CompactionResult, error) {
	model := s.Model()
	if !s.HasModel() || model == nil {
		return nil, fmt.Errorf("%s", FormatNoModelSelectedMessage())
	}
	settings, _ := s.compactionSettings()

	pathEntries := s.Sessions.GetBranch("")
	preparation := PrepareCompaction(pathEntries, settings)
	if preparation == nil {
		// Distinguish why compaction is impossible (upstream uses optional
		// chaining: an empty branch reports Nothing-to-compact, not a panic).
		if len(pathEntries) > 0 && pathEntries[len(pathEntries)-1].Type == "compaction" {
			return nil, fmt.Errorf("Already compacted")
		}
		return nil, fmt.Errorf("Nothing to compact (session too small)")
	}

	options := CompactionOptions{
		Model: model, Ctx: ctx, CustomInstructions: customInstructions,
		StreamFn: s.compactionStreamFn(), Retry: s.retrySettings(),
		SessionID: s.Sessions.GetSessionID(), ThinkingLevel: s.ThinkingLevel(),
		Prefix: s.replayPrefix(),
	}
	if s.control.ModelRuntime != nil && !s.summariesResolveAuthInStream() {
		resolution, err := s.control.ModelRuntime.GetAuthForModel(model, &ModelRuntimeAuthOverrides{Ctx: ctx})
		if err == nil && resolution != nil {
			options.APIKey = resolution.Auth.APIKey
			options.Headers = headersToStrings(resolution.Auth.Headers)
			options.Env = resolution.Env
			if resolution.Auth.BaseURL != "" {
				copied := *model
				copied.BaseURL = resolution.Auth.BaseURL
				options.Model = &copied
			}
		}
	}

	result, err := Compact(preparation, options)
	// The replay is spent either way, and the captured transcript is the
	// pre-compaction history once a summary replaces it (D419).
	s.requestPrefix.clear()
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Compaction cancelled")
	}

	s.Sessions.AppendCompaction(result.Summary, result.FirstKeptEntryID, result.TokensBefore, result.Details, false, result.Usage)
	sessionContext := s.replaceAgentMessagesFromSession()
	result.EstimatedTokensAfter = int64(estimateMessagesTokens(sessionContext.Messages))
	s.refreshFinalizedContext(sessionContext)
	return result, nil
}

// estimateMessagesTokens sums the estimated size of every message
// (upstream estimateMessagesTokens).
func estimateMessagesTokens(messages []ai.Message) int {
	tokens := 0
	for _, message := range messages {
		tokens += EstimateAgentMessageTokens(message)
	}
	return tokens
}

// refreshFinalizedContext records the finalized context's message-to-entry
// mapping (upstream _refreshFinalizedContext).
func (s *AgentSession) refreshFinalizedContext(context SessionContext) {
	ids := make(map[ai.Message]string, len(context.Messages))
	for _, entry := range s.Sessions.ProjectedEntries() {
		for _, message := range entry.Messages {
			ids[message] = entry.Source.ID
		}
	}
	s.mu.Lock()
	s.entryIDsByMessage = ids
	s.mu.Unlock()
}

// adaptStreamFn wraps a StreamFn into the compaction StreamFnFn shape.
func adaptStreamFn(streamFn agent.StreamFn) StreamFnFn {
	if streamFn == nil {
		return nil
	}
	return func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		return streamFn(model, context, options)
	}
}

// maybeAutoCompact checks the threshold over the complete projected context
// and compacts when crossed (the prepareNextTurn hook upstream:
// _compactBeforeNextAssistantResponse -> _exceedsCompactionThreshold). The
// estimate covers every projected message, including tool results appended
// after the last provider usage, not only the usage of the last assistant.
func (s *AgentSession) maybeAutoCompact(ctx context.Context) error {
	settings, enabled := s.compactionSettings()
	if !enabled {
		return nil
	}
	model := s.Model()
	if model == nil || model.ContextWindow <= 0 {
		return nil
	}
	contextTokens := int64(s.Sessions.ProjectedContextEstimate().Tokens)
	if ShouldCompact(contextTokens, model.ContextWindow, settings) {
		_, err := s.RunAutoCompaction(ctx, CompactionThreshold, false)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// SessionStats is the session counters report.
type SessionStats struct {
	SessionFile       string
	SessionID         string
	UserMessages      int
	AssistantMessages int
	ToolCalls         int
	ToolResults       int
	TotalMessages     int
	Tokens            struct {
		Input      int64
		Output     int64
		CacheRead  int64
		CacheWrite int64
		Total      int64
	}
	Cost float64
	// ContextUsage is getContextUsage()'s report (upstream getSessionStats
	// returns it unchanged, so unknown tokens/percent stay null).
	ContextUsage *ContextUsageReport
}

// GetSessionStats aggregates the session entries (port of getSessionStats). The
// counts, tool calls and token totals come folded from the session manager; only
// the context usage needs the live agent state, which the port reads here.
func (s *AgentSession) GetSessionStats() *SessionStats {
	stats := s.Sessions.SessionStats()
	stats.ContextUsage = s.GetContextUsage()
	return stats
}
