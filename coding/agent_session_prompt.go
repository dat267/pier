package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Port of the prompt pipeline of core/agent-session.ts: prompt with streaming
// behaviors, skill/prompt-template expansion, pending-message flushing, model
// and auth validation, pre-send compaction, custom-message buffering,
// sendUserMessage, abort, and waitForIdle.
//
// D41: the extension input handlers, extension commands, and before-agent-start
// hooks are extension mechanics and omitted.

// PromptOptions configure a prompt submission.
type PromptOptions struct {
	// ExpandPromptTemplates expands /skill: and /template commands (default true).
	ExpandPromptTemplates *bool
	// StreamingBehavior queues the prompt when a run is active: "steer" or
	// "followUp". Without it, prompting during a run is an error.
	StreamingBehavior string
	// Images are image attachments (upstream ImageContent[]).
	Images []ai.ImageContent
	// Source labels the input origin ("interactive", "extension", "rpc").
	Source string
	// SessionID is the request's session id; warming restarts only from
	// session requests (compaction and summaries use their own routing ids).
	SessionID string
	// PreflightResult observes whether the prompt was queued/executed (true) or
	// dropped (false).
	PreflightResult func(ok bool)
	// OnAccepted reports "started" or "queued" after successful preflight.
	// Extensions are absent (D41), so "handled" is never produced.
	OnAccepted func(disposition string)
}

// InputSource names where user input came from.
type InputSource = string

const (
	InputSourceExtension InputSource = "extension"
)

// promptState is the session's prompt buffering state.
type promptState struct {
	mu sync.Mutex
	// pendingCustomMessages await the end of the current turn.
	// D226: a custom message that lands between a tool call and its result is
	// deferred here, so ConvertToLlm never turns it into a user message that
	// interrupts the tool flow.
	pendingCustomMessages []*ai.CustomMessage
	// pendingNextTurnMessages are injected with the next user prompt.
	pendingNextTurnMessages []ai.Message
	// runActive mirrors upstream's _isAgentRunActive.
	runActive bool
	// Run lifetime spans low-level agent runs, backoff and continuations.
	abortRequested bool
	runCancel      context.CancelFunc
	// idleWait resolves once the session becomes idle.
	idleWait chan struct{}
	// disposed is set by Dispose. It lives under the same lock as runActive
	// and runCancel, so a guarded run start (runAgentPromptUnlessDisposed)
	// and Dispose's mark-then-abort cannot interleave: either the start sees
	// the mark, or the abort sees the started run's cancel.
	disposed bool
}

// isDisposed reports whether Dispose has run.
func (s *AgentSession) isDisposed() bool {
	state := s.prompt()
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.disposed
}

func (s *AgentSession) prompt() *promptState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.promptState == nil {
		s.promptState = &promptState{}
	}
	return s.promptState
}

// cacheContextIsCurrent reports whether the live transcript still extends the
// warmed request's prefix by message identity (upstream cacheContextIsCurrent).
func (s *AgentSession) cacheContextIsCurrent() func() bool {
	return s.Agent.MessagePrefixIsCurrent(s.Model())
}

// GetCacheWarmingStatus reports the warmer's state.
func (s *AgentSession) GetCacheWarmingStatus() *CacheWarmingStatus {
	if s.CacheWarmer == nil {
		return nil
	}
	status := s.CacheWarmer.Status()
	return &status
}

// SetCacheWarmingMode persists the warming mode and reconciles the warmer.
func (s *AgentSession) SetCacheWarmingMode(mode CacheWarmingMode) {
	if s.control.Settings != nil {
		s.control.Settings.SetCacheWarmingMode(mode)
	}
	if s.CacheWarmer != nil {
		s.CacheWarmer.OnModeChanged()
	}
}

// normalizePromptImages runs prompt image attachments through ProcessImage so
// the request's resize profile applies (the imageAutoResize setting and the
// active model's image limits) and returns the per-image hints/omission notes.
// Port of AgentSession._normalizePromptImages; queueing paths (steer/follow-up)
// stay raw, matching upstream.
func (s *AgentSession) normalizePromptImages(images []ai.ImageContent) ([]ai.ImageContent, []string) {
	if len(images) == 0 {
		return nil, nil
	}
	autoResize := true
	if s.control.Settings != nil {
		autoResize = s.control.Settings.GetImageAutoResize()
	}
	resizeOptions := imageResizeOptionsFor(s.Model())
	var normalized []ai.ImageContent
	var hints []string
	for _, image := range images {
		data, ok := DecodeImageBase64(image.Data)
		if !ok {
			// Upstream decodes base64 leniently; keep an undecodable block
			// rather than dropping it.
			normalized = append(normalized, image)
			continue
		}
		processed := ProcessImage(data, image.MimeType, &ProcessImageOptions{
			AutoResizeImages: &autoResize,
			ResizeOptions:    &resizeOptions,
		})
		if !processed.OK {
			hints = append(hints, processed.Message)
			continue
		}
		normalized = append(normalized, ai.ImageContent{Data: processed.Data, MimeType: processed.MimeType})
		hints = append(hints, processed.Hints...)
	}
	return normalized, hints
}

// Prompt submits text to the session, expanding skill commands and prompt
// templates and honoring the streaming behavior.
func (s *AgentSession) Prompt(ctx context.Context, text string, options *PromptOptions) error {
	if options == nil {
		options = &PromptOptions{}
	}
	expandTemplates := options.ExpandPromptTemplates == nil || *options.ExpandPromptTemplates
	preflight := func(ok bool) {
		if options.PreflightResult != nil {
			options.PreflightResult(ok)
		}
	}

	if s.IsCompacting() {
		preflight(false)
		return fmt.Errorf("Cannot submit a prompt while compaction is in progress. Wait for compaction to finish and retry.")
	}

	expandedText := text
	if expandTemplates {
		expandedText = s.expandSkillCommand(expandedText)
		expandedText = ExpandPromptTemplate(expandedText, s.PromptTemplates())
	}

	// While streaming, the prompt must be queued with an explicit behavior.
	if s.IsStreaming() {
		if options.StreamingBehavior == "" {
			preflight(false)
			return fmt.Errorf("Agent is already processing. Specify streamingBehavior ('steer' or 'followUp') to queue the message.")
		}
		if options.StreamingBehavior == "followUp" {
			s.queueFollowUp(expandedText, options.Images)
		} else {
			s.queueSteer(expandedText, options.Images)
		}
		preflight(true)
		if options.OnAccepted != nil {
			options.OnAccepted("queued")
		}
		return nil
	}

	// Flush deferred messages before the new turn.
	s.FlushPendingBashMessages()
	s.FlushPendingCustomMessages()

	if !s.HasModel() {
		preflight(false)
		return fmt.Errorf("%s", FormatNoModelSelectedMessage())
	}
	if err := s.validateModelAuth(ctx); err != nil {
		preflight(false)
		return err
	}

	// Catch an aborted response before sending the new prompt.
	if last := s.findLastAssistantMessage(); last != nil {
		if err := s.checkCompaction(ctx, last, false); err != nil {
			preflight(false)
			return err
		}
	}

	// Build the prompt: the user message plus any pending next-turn messages.
	// The images are normalized before the message is built so the resize
	// profile of the model that will serve the request applies (upstream
	// normalizes after before_agent_start, whose hooks can pick the model).
	normalizedImages, imageHints := s.normalizePromptImages(options.Images)
	userText := expandedText
	if len(imageHints) > 0 {
		userText = expandedText + "\n\n" + strings.Join(imageHints, "\n")
	}
	userContent := ai.ContentList{ai.TextContent{Text: userText}}
	for _, image := range normalizedImages {
		userContent = append(userContent, image)
	}
	messages := []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: userContent}, Timestamp: time.Now().UnixMilli()}}
	messages = append(messages, s.takePendingNextTurnMessages()...)
	// Install the prompt/tool loadout as a leading system message so the model
	// receives the built sections (upstream _preparePromptAndToolLoadout).
	if update := s.preparePromptAndToolLoadout(); update != nil {
		messages = append([]ai.Message{update}, messages...)
	}

	if s.CacheWarmer != nil && options.SessionID != "" && options.SessionID == s.SessionID() {
		requestOptions := &ai.SimpleStreamOptions{SessionID: options.SessionID}
		if len(options.Images) > 0 {
			// Images ride in the user message; the warm request replays the same
			// context without them only when the model accepts it, so keep them.
		}
		s.CacheWarmer.Start(CacheWarmRequest{
			Model: s.Model(), Context: ai.Context{Messages: messages}, Options: requestOptions,
		}, s.cacheContextIsCurrent())
	}
	return s.runAgentPrompt(ctx, messages, func() {
		preflight(true)
		if options.OnAccepted != nil {
			options.OnAccepted("started")
		}
	})
}

func (s *AgentSession) queueSteer(text string, images []ai.ImageContent) {
	content := ai.ContentList{ai.TextContent{Text: text}}
	for _, image := range images {
		content = append(content, image)
	}
	s.Steer(&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: content}, Timestamp: time.Now().UnixMilli()})
}

func (s *AgentSession) queueFollowUp(text string, images []ai.ImageContent) {
	content := ai.ContentList{ai.TextContent{Text: text}}
	for _, image := range images {
		content = append(content, image)
	}
	s.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: content}, Timestamp: time.Now().UnixMilli()})
}

// validateModelAuth checks that the selected model's provider has usable auth.
func (s *AgentSession) validateModelAuth(ctx context.Context) error {
	if s.control.ModelRuntime == nil {
		return nil
	}
	runtime := s.control.ModelRuntime
	model := s.Model()
	if runtime.HasConfiguredAuth(model.Provider) {
		return nil
	}
	if check, err := runtime.CheckAuth(model.Provider, ctx); err == nil && check != nil {
		return nil
	}
	if runtime.IsUsingOAuth(model.Provider) {
		return fmt.Errorf("Authentication failed for \"%s\". Credentials may have expired or network is unavailable. Run '/login %s' to re-authenticate.",
			model.Provider, model.Provider)
	}
	return fmt.Errorf("%s", FormatNoAPIKeyFoundMessage(model.Provider))
}

// runAgentPrompt runs one agent prompt with the run-active flag and idle
// resolution.
func (s *AgentSession) runAgentPrompt(ctx context.Context, messages []ai.Message, onStarted ...func()) error {
	return s.startAgentPrompt(ctx, messages, false, onStarted...)
}

// runAgentPromptUnlessDisposed is runAgentPrompt for work the host runs later
// (released follow-ups): it starts nothing once Dispose has marked the
// session, checking in the same critical section that installs the run.
func (s *AgentSession) runAgentPromptUnlessDisposed(ctx context.Context, messages []ai.Message) error {
	return s.startAgentPrompt(ctx, messages, true)
}

func (s *AgentSession) startAgentPrompt(ctx context.Context, messages []ai.Message, unlessDisposed bool, onStarted ...func()) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := s.prompt()
	state.mu.Lock()
	if unlessDisposed && state.disposed {
		state.mu.Unlock()
		return nil
	}
	state.abortRequested = false
	state.runCancel = cancel
	state.runActive = true
	state.mu.Unlock()
	// The hold latch is per run: a goal active during this run keeps user
	// follow-ups queued through its post-run continuation. A run that starts
	// consumes the pending release this flag reported.
	s.goalHeldThisRun.Store(false)
	s.followUpReleasePending.Store(false)
	// Preparation and user callbacks (onStarted, the pre-turn hook) can panic.
	// Upstream's _startRun catch still clears the run flag and releases idle
	// waiters; without this, one panic wedges the session in streaming.
	defer func() {
		if recovered := recover(); recovered != nil {
			s.releaseRunAfterPanic()
			panic(recovered)
		}
	}()
	for _, notify := range onStarted {
		notify()
	}
	// The once-per-session pre-turn hook (upstream before_agent_start): the
	// MCP first-prompt wait installs here, so the first request carries the
	// tools that connected in time.
	s.mu.Lock()
	hook := s.beforeFirstTurn
	s.mu.Unlock()
	if hook != nil {
		s.firstTurnOnce.Do(func() {
			s.mu.Lock()
			s.firstTurnRan = true
			s.mu.Unlock()
			hook(ctx)
		})
	}

	err := s.Agent.PromptMessages(ctx, messages)
	if err == nil {
		// Post-run continuation: auto-retry, then queued continuations
		// (upstream's _runAgentPrompt loop).
		err = s.runPostRunLoop(ctx)
	}

	// Settlement still belongs to this run: upstream flushes pending messages
	// and notifies the cache warmer before _emitAgentSettled clears the run
	// flag, so a new prompt cannot start while the old run is appending context.
	s.FlushPendingBashMessages()
	s.FlushPendingCustomMessages()
	if s.CacheWarmer != nil {
		s.CacheWarmer.OnAgentSettled()
	}
	state.mu.Lock()
	state.runActive = false
	state.runCancel = nil
	aborted := state.abortRequested
	state.mu.Unlock()
	// Upstream _emitAgentSettled uses the run abort flag, including backoff.
	s.emit(&SessionEvent{
		Type: SessionAgentSettled, Aborted: aborted,
		GoalPursuing:     s.Goal != nil && s.Goal.Pursuing(),
		FollowUpsPending: s.followUpReleasePending.Load(),
	})
	s.resolveIdleWaitIfIdle()
	return err
}

// releaseRunAfterPanic unwinds a run whose preparation or callbacks panicked.
// It mirrors _startRun's catch: no agent_settled is emitted, but the run flag
// is cleared, deferred context is flushed and idle waiters are released.
func (s *AgentSession) releaseRunAfterPanic() {
	state := s.prompt()
	state.mu.Lock()
	active := state.runActive
	state.runActive = false
	state.runCancel = nil
	state.mu.Unlock()
	if active {
		s.FlushPendingBashMessages()
		s.FlushPendingCustomMessages()
		if s.CacheWarmer != nil {
			s.CacheWarmer.OnAgentSettled()
		}
	}
	s.resolveIdleWaitIfIdle()
}

// findLastAssistantMessage returns the most recent assistant message.
func (s *AgentSession) findLastAssistantMessage() *ai.AssistantMessage {
	messages := s.Messages()
	for _, message := range slices.Backward(messages) {
		if assistant, ok := message.(*ai.AssistantMessage); ok {
			return assistant
		}
	}
	return nil
}

// checkCompaction runs the automatic compaction decision (upstream
// _checkCompaction; skipAbortedCheck=false is the pre-prompt check).
func (s *AgentSession) checkCompaction(ctx context.Context, message *ai.AssistantMessage, skipAbortedCheck bool) error {
	_, err := s.CheckCompaction(ctx, message, skipAbortedCheck)
	return err
}

// SendUserMessage sends a user message, optionally delivering it as a queued
// steer/follow-up message.
func (s *AgentSession) SendUserMessage(ctx context.Context, content any, deliverAs string) error {
	var text string
	var images []ai.ImageContent
	switch typed := content.(type) {
	case string:
		text = typed
	case ai.ContentList:
		var parts []string
		for _, block := range typed {
			switch value := block.(type) {
			case ai.TextContent:
				parts = append(parts, value.Text)
			case ai.ImageContent:
				images = append(images, value)
			}
		}
		text = strings.Join(parts, "\n")
	}
	expand := false
	return s.Prompt(ctx, text, &PromptOptions{
		ExpandPromptTemplates: &expand, StreamingBehavior: deliverAs, Images: images, Source: InputSourceExtension,
	})
}

// SendMessageOptions configure SendMessage.
type SendMessageOptions struct {
	// TriggerTurn starts a turn for the message (default true).
	TriggerTurn *bool
	// DeliverAs queues as "steer" or "followUp" when a run is active.
	DeliverAs string
	// unlessDisposed starts the turn only if Dispose has not marked the
	// session, checked where the run is installed (deferred goal rounds).
	unlessDisposed bool
}

// SendMessage injects a custom message into the conversation.
func (s *AgentSession) SendMessage(ctx context.Context, message *ai.CustomMessage, options *SendMessageOptions) error {
	if options == nil {
		options = &SendMessageOptions{}
	}
	triggerTurn := options.TriggerTurn == nil || *options.TriggerTurn

	state := s.prompt()
	switch {
	case triggerTurn && !s.IsStreaming():
		return s.startAgentPrompt(ctx, []ai.Message{message}, options.unlessDisposed)
	case s.IsStreaming():
		if !triggerTurn {
			// Appending now would sit between a tool call and its result.
			state.mu.Lock()
			state.pendingCustomMessages = append(state.pendingCustomMessages, message)
			state.mu.Unlock()
			return nil
		}
		if options.DeliverAs == "followUp" {
			s.FollowUp(message)
		} else {
			s.Steer(message)
		}
		return nil
	default:
		s.AppendCustomMessage(message)
		return nil
	}
}

// AppendCustomMessage appends a custom message to the transcript and session.
func (s *AgentSession) AppendCustomMessage(message *ai.CustomMessage) {
	s.Agent.AppendMessages(message)
	var fields customMessageFields
	if err := json.Unmarshal(message.Content, &fields); err == nil && fields.CustomType != "" {
		s.Sessions.AppendCustomMessageEntry(fields.CustomType, fields.Content, fields.Display, fields.Details)
	} else {
		s.Sessions.AppendCustomMessageEntry(message.Role, message.Content, false, nil)
	}
	// The custom entry is already persisted. Publish directly rather than
	// passing through handleAgentEvent, which would append it a second time.
	s.emit(&SessionEvent{Type: SessionMessageStart, Agent: &agent.AgentEvent{Type: agent.MessageStart, Message: message}})
	s.emit(&SessionEvent{Type: SessionMessageEnd, Agent: &agent.AgentEvent{Type: agent.MessageEnd, Message: message}})
}

// FlushPendingCustomMessages appends custom messages queued during a run.
func (s *AgentSession) FlushPendingCustomMessages() {
	state := s.prompt()
	state.mu.Lock()
	pending := state.pendingCustomMessages
	state.pendingCustomMessages = nil
	state.mu.Unlock()
	for _, message := range pending {
		s.AppendCustomMessage(message)
	}
}

// QueueNextTurnMessage queues a message injected alongside the next prompt.
func (s *AgentSession) QueueNextTurnMessage(message ai.Message) {
	state := s.prompt()
	state.mu.Lock()
	state.pendingNextTurnMessages = append(state.pendingNextTurnMessages, message)
	state.mu.Unlock()
}

func (s *AgentSession) takePendingNextTurnMessages() []ai.Message {
	state := s.prompt()
	state.mu.Lock()
	defer state.mu.Unlock()
	pending := state.pendingNextTurnMessages
	state.pendingNextTurnMessages = nil
	return pending
}

// Abort aborts the current operation and waits for idle. Callers that need the
// session settled before they continue (session switch, tree navigation) use
// this; a caller on the UI loop must not, because the wait parks the loop (D168).
//
// Like upstream session.abort(), it stops the turn (agent run, retry,
// compaction, branch summary) but not a user bash command, which keeps running
// and reports when the turn ends; Dispose and AbortAsync abort bash too.
func (s *AgentSession) Abort(ctx context.Context) {
	s.abortAsync(false)
	_ = s.waitForSessionIdle(ctx)
}

// AbortAsync aborts the current operation without waiting for the session to go
// idle. The wait in Abort is what the UI loop cannot afford: the Escape handler
// restores the queued messages, then aborts, so waiting there stalled every later
// keystroke for as long as the in-flight turn took to unwind — 188 ms measured
// mid-turn (`slow UI phase "raw-input" 188ms`, loop parked in waitForSessionIdle),
// which upstream never pays because awaiting a promise yields to its event loop
// (D168).
func (s *AgentSession) AbortAsync() { s.abortAsync(true) }

// AbortTurnAsync aborts the agent run, retry and compaction work without
// cancelling independently running user Bash commands (as RPC `abort` requires).
func (s *AgentSession) AbortTurnAsync() { s.abortAsync(false) }

func (s *AgentSession) abortAsync(abortBash bool) {
	state := s.prompt()
	state.mu.Lock()
	if state.runActive {
		state.abortRequested = true
	}
	cancel := state.runCancel
	state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.AbortRetry()
	s.AbortCompaction()
	s.AbortBranchSummary()
	if abortBash {
		s.AbortBash()
	}
	s.Agent.Abort()
}

// waitForSessionIdle blocks until no agent run or compaction is active,
// resolving as soon as either finishes.
func (s *AgentSession) waitForSessionIdle(ctx context.Context) error {
	wait := s.registerIdleWait()
	if wait == nil {
		return nil
	}

	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// registerIdleWait installs the channel completion closes. Registration is
// separate from waitForSessionIdle's idle check so a completion between the
// two cannot be lost: the caller rechecks after registering.
func (s *AgentSession) registerIdleWait() chan struct{} {
	state := s.prompt()
	state.mu.Lock()
	if state.idleWait == nil {
		state.idleWait = make(chan struct{})
	}
	wait := state.idleWait
	state.mu.Unlock()
	// Recheck after publishing the channel. A completion that happened between
	// the caller's idle check and this registration would otherwise find no
	// waiter, and the channel below would never close.
	if s.IsIdle() {
		s.resolveIdleWaitIfIdle()
		return nil
	}
	return wait
}

// resolveIdleWaitIfIdle releases the idle waiters once nothing is active.
func (s *AgentSession) resolveIdleWaitIfIdle() {
	if !s.IsIdle() {
		return
	}
	state := s.prompt()
	state.mu.Lock()
	wait := state.idleWait
	state.idleWait = nil
	state.mu.Unlock()
	if wait != nil {
		close(wait)
	}
}

// expandSkillCommand expands a /skill:name command into its skill block.
func (s *AgentSession) expandSkillCommand(text string) string {
	if !strings.HasPrefix(text, "/skill:") {
		return text
	}
	spaceIndex := strings.Index(text, " ")
	skillName := text[len("/skill:"):]
	args := ""
	if spaceIndex != -1 {
		skillName = text[len("/skill:"):spaceIndex]
		args = ai.JSTrim(text[spaceIndex+1:])
	}

	var skill *Skill
	if options, ok := s.systemPromptOptionsSnapshot(); ok {
		for index := range options.Skills {
			if options.Skills[index].Name == skillName {
				skill = &options.Skills[index]
				break
			}
		}
	}
	if skill == nil {
		return text
	}
	content, err := os.ReadFile(skill.FilePath)
	if err != nil {
		return text
	}
	body := ai.JSTrim(StripFrontmatter(DecodeUTF8WithReplacement(content)))
	block := fmt.Sprintf("<skill name=\"%s\" location=\"%s\">\nReferences are relative to %s.\n\n%s\n</skill>",
		skill.Name, skill.FilePath, skill.BaseDir, body)
	if args != "" {
		return block + "\n\n" + args
	}
	return block
}
