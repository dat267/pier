package coding

// Port of pi v1.3.0 modes/rpc/rpc-mode.ts. This runner exposes the
// extension-free stdio command surface, not packages/server's CBOR protocol.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/dat267/pier/ai"
)

// RPCModeOptions supplies the owned input and protocol output streams.
// Input is closed on teardown. Output must remain reserved for this run:
// cancellation can release the owner before an uninterruptible OS write ends.
type RPCModeOptions struct {
	Input  io.ReadCloser
	Output io.Writer
	// ReplaceSession builds the session for a replacement SessionManager
	// (new_session, switch_session, fork, clone). The runner disposes the
	// current session before calling it and subscribes the result. When nil,
	// replacement commands fail explicitly.
	ReplaceSession func(ctx context.Context, manager *SessionManager) (*AgentSession, error)
	// ExportToHtml renders the session branch to an HTML file and returns the
	// resolved path (upstream agent-session.exportToHtml). The interactive
	// package owns the renderer, so the host injects it. When nil, export_html
	// fails explicitly.
	ExportToHtml func(session *AgentSession, outputPath string) (string, error)
}

type rpcCommand struct {
	ID                 *string           `json:"id"`
	Type               string            `json:"type"`
	Message            *string           `json:"message"`
	Provider           string            `json:"provider"`
	ModelID            string            `json:"modelId"`
	Level              string            `json:"level"`
	Mode               string            `json:"mode"`
	Images             []ai.ImageContent `json:"images"`
	StreamingBehavior  string            `json:"streamingBehavior"`
	CustomInstructions *string           `json:"customInstructions"`
	Enabled            *bool             `json:"enabled"`
	Command            *string           `json:"command"`
	ExcludeFromContext *bool             `json:"excludeFromContext"`
	Since              *string           `json:"since"`
	Name               *string           `json:"name"`
	ParentSession      *string           `json:"parentSession"`
	SessionPath        *string           `json:"sessionPath"`
	EntryID            *string           `json:"entryId"`
	Position           *string           `json:"position"`
	OutputPath         *string           `json:"outputPath"`
}

type rpcSessionState struct {
	Model                 *ai.Model `json:"model,omitempty"`
	ThinkingLevel         string    `json:"thinkingLevel"`
	IsStreaming           bool      `json:"isStreaming"`
	IsCompacting          bool      `json:"isCompacting"`
	SteeringMode          string    `json:"steeringMode"`
	FollowUpMode          string    `json:"followUpMode"`
	SessionFile           string    `json:"sessionFile,omitempty"`
	SessionID             string    `json:"sessionId"`
	SessionName           string    `json:"sessionName,omitempty"`
	AutoCompactionEnabled bool      `json:"autoCompactionEnabled"`
	MessageCount          int       `json:"messageCount"`
	PendingMessageCount   int       `json:"pendingMessageCount"`
}

type rpcPacket struct {
	data []byte
	done chan error
}

type rpcRuntime struct {
	ctx            context.Context
	cancel         context.CancelFunc
	session        *AgentSession
	replaceSession func(context.Context, *SessionManager) (*AgentSession, error)
	exportToHtml   func(*AgentSession, string) (string, error)
	unsubscribe    func()
	packets        chan rpcPacket
	outputMu       sync.Mutex
	outputErr      error
	workers        sync.WaitGroup
	runCtx         context.Context
	runCancel      context.CancelFunc
	admission      <-chan struct{}
	promptSlots    chan struct{}
}

// RunRPCMode processes LF-delimited commands until EOF. It owns session
// teardown and drains accepted session writes before returning.
func RunRPCMode(ctx context.Context, session *AgentSession, options RPCModeOptions) (err error) {
	// Materialize lazy run bookkeeping before the owner and prompt workers
	// share the session; its pointer is then stable for state queries.
	session.prompt()
	ctx, cancel := context.WithCancel(ctx)
	runtime := &rpcRuntime{
		ctx: ctx, cancel: cancel, session: session, replaceSession: options.ReplaceSession,
		exportToHtml: options.ExportToHtml,
		packets:      make(chan rpcPacket), promptSlots: make(chan struct{}, 4),
	}
	runtime.bindSession()
	go runtime.writePackets(options.Output)
	// A standard-input file can be in a blocking OS read that Close cannot
	// interrupt on every platform. D235: cancellation releases the owner
	// independently; this reader only forwards bytes and never touches session
	// state. Closing an ordinary pipe releases it immediately.
	context.AfterFunc(ctx, func() { _ = options.Input.Close() })
	defer func() {
		cancel()
		runtime.session.AbortAsync()
		runtime.workers.Wait()
		runtime.unsubscribe()
		runtime.session.Abort(context.Background())
		runtime.session.Dispose()
		if outputErr := runtime.outputError(); outputErr != nil {
			err = outputErr
		}
		if writeErr := runtime.session.Sessions.FlushWrites(); err == nil {
			err = writeErr
		}
	}()
	type inputLine struct {
		data []byte
		err  error
	}
	lines := make(chan inputLine)
	go func() {
		reader := bufio.NewReader(options.Input)
		for {
			line, readErr := reader.ReadBytes('\n')
			select {
			case lines <- inputLine{line, readErr}:
			case <-ctx.Done():
				return
			}
			if readErr != nil {
				return
			}
		}
	}()
	for {
		var input inputLine
		select {
		case input = <-lines:
		case <-ctx.Done():
			return ctx.Err()
		}
		line, readErr := input.data, input.err
		if len(line) != 0 {
			if err := runtime.handleLine(line); err != nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (r *rpcRuntime) handleLine(line []byte) error {
	var object map[string]json.RawMessage
	parseErr := json.Unmarshal(line, &object)
	command := rpcCommand{}
	if parseErr == nil {
		if raw, ok := object["type"]; ok {
			parseErr = json.Unmarshal(raw, &command.Type)
		}
		if parseErr == nil && command.Type == "" {
			parseErr = fmt.Errorf("command must be an object with a nonempty type")
		}
		if parseErr == nil {
			if raw, ok := object["id"]; ok {
				parseErr = json.Unmarshal(raw, &command.ID)
			}
		}
	}
	if parseErr != nil {
		return r.respond(rpcCommand{Type: "parse"}, nil, false, fmt.Errorf("Failed to parse command: %w", parseErr))
	}
	// D235: validate untrusted fields without losing a valid correlation id.
	// Explicit property lookup also preserves JavaScript's case sensitivity.
	for _, field := range []struct {
		name   string
		target any
	}{
		{"message", &command.Message}, {"provider", &command.Provider},
		{"modelId", &command.ModelID}, {"level", &command.Level},
		{"mode", &command.Mode}, {"images", &command.Images},
		{"streamingBehavior", &command.StreamingBehavior},
		{"customInstructions", &command.CustomInstructions},
		{"enabled", &command.Enabled},
		{"command", &command.Command},
		{"excludeFromContext", &command.ExcludeFromContext},
		{"since", &command.Since},
		{"name", &command.Name},
		{"parentSession", &command.ParentSession},
		{"sessionPath", &command.SessionPath},
		{"entryId", &command.EntryID},
		{"position", &command.Position},
		{"outputPath", &command.OutputPath},
	} {
		if raw, ok := object[field.name]; ok {
			if err := json.Unmarshal(raw, field.target); err != nil {
				return r.respond(command, nil, false, fmt.Errorf("Invalid %s: %w", field.name, err))
			}
		}
	}
	if err := command.validate(); err != nil {
		return r.respond(command, nil, false, err)
	}
	if command.Type == "prompt" {
		return r.startPrompt(command)
	}
	// Commands that can block for a long time run off the read loop so a later
	// abort/abort_bash can cancel them (upstream's fire-and-forget
	// handleInputLine does the same).
	if command.Type == "bash" || command.Type == "compact" || command.Type == "export_html" {
		return r.startBlocking(command)
	}
	data, hasData, commandErr := r.handle(command)
	return r.respond(command, data, hasData, commandErr)
}

func (command rpcCommand) validate() error {
	switch command.Type {
	case "prompt", "steer", "follow_up":
		if command.Message == nil {
			return fmt.Errorf("message must be a string")
		}
		if command.StreamingBehavior != "" && command.StreamingBehavior != "steer" && command.StreamingBehavior != "followUp" {
			return fmt.Errorf("streamingBehavior must be steer or followUp")
		}
	case "set_thinking_level":
		if !IsValidThinkingLevel(command.Level) {
			return fmt.Errorf("level must be a valid thinking level")
		}
	case "set_model":
		if command.Provider == "" || command.ModelID == "" {
			return fmt.Errorf("provider and modelId must be nonempty strings")
		}
	case "set_session_name":
		if command.Name == nil {
			return fmt.Errorf("name must be a string")
		}
	case "bash":
		if command.Command == nil {
			return fmt.Errorf("command must be a string")
		}
	case "set_auto_compaction", "set_auto_retry":
		if command.Enabled == nil {
			return fmt.Errorf("enabled must be a boolean")
		}
	case "switch_session":
		if command.SessionPath == nil {
			return fmt.Errorf("sessionPath must be a string")
		}
	case "fork":
		if command.EntryID == nil {
			return fmt.Errorf("entryId must be a string")
		}
	}
	return nil
}

func (r *rpcRuntime) respond(command rpcCommand, data any, hasData bool, commandErr error) error {
	response := jsonEventObject{}
	if command.ID != nil {
		response.add("id", *command.ID)
	}
	response.add("type", "response")
	response.add("command", command.Type)
	response.add("success", commandErr == nil)
	if commandErr != nil {
		response.add("error", commandErr.Error())
	} else if hasData {
		response.add("data", data)
	}
	encoded, err := ai.MarshalJSON(response)
	if err != nil {
		return err
	}
	return r.write(encoded)
}

func (r *rpcRuntime) fail(err error) {
	r.outputMu.Lock()
	if r.outputErr == nil {
		r.outputErr = err
	}
	r.outputMu.Unlock()
	r.cancel()
}

func (r *rpcRuntime) outputError() error {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	return r.outputErr
}

// bindSession subscribes the runtime's event forwarder to the current session.
func (r *rpcRuntime) bindSession() {
	r.unsubscribe = r.session.Subscribe(func(event *SessionEvent) {
		encoded, encodeErr := MarshalJSONSessionEvent(event)
		if encodeErr != nil {
			r.fail(encodeErr)
			return
		}
		_ = r.write(encoded)
	})
}

// settleWorkers cancels the current run, aborts bash/compaction/retry and waits
// for in-flight command workers. It runs on the command owner, never a worker.
func (r *rpcRuntime) settleWorkers() {
	if r.runCancel != nil {
		r.runCancel()
	}
	r.session.AbortCompaction()
	r.session.AbortBranchSummary()
	r.session.AbortAsync()
	r.workers.Wait()
}

// installSession replaces the runtime's session (upstream teardownCurrent +
// createRuntime + rebindCurrentSession): it settles and unsubscribes the old
// session, builds a new one through the host factory, then disposes the old
// session and subscribes the replacement. A failed build keeps the settled old
// session bound so the client can continue.
func (r *rpcRuntime) installSession(ctx context.Context, manager *SessionManager) error {
	if r.replaceSession == nil {
		return fmt.Errorf("Session replacement is not available in this build")
	}
	if r.unsubscribe != nil {
		r.unsubscribe()
		r.unsubscribe = nil
	}
	r.settleWorkers()
	created, err := r.replaceSession(ctx, manager)
	if err != nil {
		// Creation failed: keep the (settled) old session bound so the client
		// can continue, rather than leaving the runtime without a session.
		r.bindSession()
		return err
	}
	r.session.Dispose()
	created.prompt()
	r.session = created
	r.bindSession()
	return nil
}

// newSessionManager mirrors upstream newSession: a fresh persisted manager in
// the current session directory, or an in-memory one when the session is not
// persisted.
func (r *rpcRuntime) newSessionManager() *SessionManager {
	current := r.session.Sessions
	persist := current.IsPersisted()
	return NewSessionManager(current.GetCwd(), &SessionManagerOptions{
		SessionDir: current.GetSessionDir(), Persist: &persist, WriteQueue: current.GetWriteQueue(),
	})
}

// forkManager builds the manager for fork/clone (upstream AgentSessionRuntime
// .fork). It returns the selected user text for a "before" fork, or nil for
// "at".
func (r *rpcRuntime) forkManager(entryID string, atPosition bool) (*SessionManager, *string, error) {
	current := r.session.Sessions
	entry := current.GetEntry(entryID)
	if entry == nil {
		return nil, nil, fmt.Errorf("Invalid entry ID for forking")
	}
	targetLeafID := entryID
	var selectedText *string
	if !atPosition {
		text, ok := rpcUserMessageEntryText(entry)
		if !ok {
			return nil, nil, fmt.Errorf("Invalid entry ID for forking")
		}
		selectedText = &text
		targetLeafID = ""
		if entry.ParentID != nil {
			targetLeafID = *entry.ParentID
		}
	}

	// In-memory sessions branch in place (upstream reuses the same manager).
	if !current.IsPersisted() {
		if targetLeafID == "" {
			current.NewSession(nil)
		} else if _, err := current.CreateBranchedSession(targetLeafID); err != nil {
			return nil, nil, err
		}
		return current, selectedText, nil
	}

	currentFile := current.GetSessionFile()
	if currentFile == "" {
		return nil, nil, fmt.Errorf("Persisted session is missing a session file")
	}
	if targetLeafID == "" {
		// Fork before the first message: a new session parented to the current
		// file.
		persist := true
		parent := currentFile
		manager := NewSessionManager(current.GetCwd(), &SessionManagerOptions{
			SessionDir: current.GetSessionDir(), Persist: &persist, WriteQueue: current.GetWriteQueue(),
		})
		manager.NewSession(&NewSessionOptions{ParentSession: &parent})
		return manager, selectedText, nil
	}
	if _, err := os.Stat(currentFile); err != nil {
		return nil, nil, fmt.Errorf("This session has not been saved yet. Send a message before cloning or forking it.")
	}
	manager, err := OpenSession(currentFile, current.GetSessionDir(), "")
	if err != nil {
		return nil, nil, err
	}
	if _, err := manager.CreateBranchedSession(targetLeafID); err != nil {
		return nil, nil, err
	}
	manager.SetWriteQueue(current.GetWriteQueue())
	return manager, selectedText, nil
}

// rpcUserMessageEntryText returns a message entry's user text (upstream
// extractUserMessageText).
func rpcUserMessageEntryText(entry *SessionEntry) (string, bool) {
	if entry.Type != "message" {
		return "", false
	}
	message, err := ai.UnmarshalMessage(entry.Message)
	if err != nil {
		return "", false
	}
	user, ok := message.(*ai.UserMessage)
	if !ok {
		return "", false
	}
	return contentTextJoinedNoSep(user.Content), true
}

// D235: one lossless, unbuffered byte handoff backpressures event producers.
// Cancellation releases producers even if the OS writer cannot be interrupted;
// the byte-only pump never reads or mutates session state.
func (r *rpcRuntime) writePackets(output io.Writer) {
	for {
		select {
		case packet := <-r.packets:
			n, err := output.Write(packet.data)
			if err == nil && n != len(packet.data) {
				err = io.ErrShortWrite
			}
			if err != nil {
				r.fail(err)
			}
			packet.done <- err
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *rpcRuntime) write(encoded []byte) error {
	packet := rpcPacket{data: append(encoded, '\n'), done: make(chan error, 1)}
	select {
	case r.packets <- packet:
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
	select {
	case err := <-packet.done:
		return err
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

func (r *rpcRuntime) startPrompt(command rpcCommand) error {
	// D235 admits four pending preflights in addition to the accepted turn.
	select {
	case r.promptSlots <- struct{}{}:
	default:
		return r.respond(command, nil, false, fmt.Errorf("Too many pending prompts; retry after pending prompt preflight completes"))
	}
	previous := r.admission
	ready := make(chan struct{})
	r.admission = ready
	if r.runCtx == nil || r.runCtx.Err() != nil {
		r.runCtx, r.runCancel = context.WithCancel(r.ctx)
	}
	ctx := r.runCtx
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		var released sync.Once
		release := func() { released.Do(func() { close(ready); <-r.promptSlots }) }
		defer release()
		// Admission stays in input order, but only until successful preflight.
		// Abort and queries stay on the command owner while auth performs I/O.
		if previous != nil {
			select {
			case <-previous:
			case <-ctx.Done():
				_ = r.respond(command, nil, false, ctx.Err())
				return
			}
		}
		if err := ctx.Err(); err != nil {
			_ = r.respond(command, nil, false, err)
			return
		}
		accepted := false
		err := r.session.Prompt(ctx, *command.Message, &PromptOptions{
			Images: command.Images, StreamingBehavior: command.StreamingBehavior, Source: "rpc",
			OnAccepted: func(disposition string) {
				accepted = true
				_ = r.respond(command, jsonEventObject{{"disposition", disposition}}, true, nil)
				release()
			},
		})
		if !accepted {
			_ = r.respond(command, nil, false, err)
		}
	}()
	return nil
}

// startBlocking runs a command that can outlive one read-loop iteration and
// responds when it finishes.
func (r *rpcRuntime) startBlocking(command rpcCommand) error {
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		data, hasData, commandErr := r.handle(command)
		_ = r.respond(command, data, hasData, commandErr)
	}()
	return nil
}

func (r *rpcRuntime) handle(command rpcCommand) (any, bool, error) {
	session := r.session
	switch command.Type {
	case "steer", "follow_up":
		text := ExpandPromptTemplate(session.expandSkillCommand(*command.Message), session.PromptTemplates())
		if command.Type == "steer" {
			session.queueSteer(text, command.Images)
		} else {
			session.queueFollowUp(text, command.Images)
		}
		return jsonEventObject{{"disposition", "queued"}}, true, nil
	case "get_available_models":
		models := session.GetAvailableModels()
		if models == nil {
			models = []*ai.Model{}
		}
		return jsonEventObject{{"models", models}}, true, nil
	case "set_model":
		for _, model := range session.GetAvailableModels() {
			if model.Provider == command.Provider && model.ID == command.ModelID {
				if err := session.SetModel(r.ctx, model, ModelMutationOptions{}); err != nil {
					return nil, false, err
				}
				return model, true, nil
			}
		}
		return nil, false, fmt.Errorf("Model not found: %s/%s", command.Provider, command.ModelID)
	case "cycle_model":
		result, err := session.CycleModel(r.ctx, "forward", ModelMutationOptions{})
		if err != nil {
			return nil, false, err
		}
		if result == nil {
			return nil, true, nil
		}
		return jsonEventObject{{"model", result.Model}, {"thinkingLevel", result.ThinkingLevel}, {"isScoped", result.IsScoped}}, true, nil
	case "get_available_thinking_levels":
		return jsonEventObject{{"levels", session.GetAvailableThinkingLevels()}}, true, nil
	case "set_thinking_level":
		session.SetThinkingLevel(command.Level)
		return nil, false, nil
	case "cycle_thinking_level":
		level, ok := session.CycleThinkingLevel(ModelMutationOptions{})
		if !ok {
			return nil, true, nil
		}
		return jsonEventObject{{"level", level}}, true, nil
	case "set_steering_mode", "set_follow_up_mode":
		if command.Mode != QueueModeAll && command.Mode != QueueModeOneAtATime {
			return nil, false, fmt.Errorf("mode must be all or one-at-a-time")
		}
		if command.Type == "set_steering_mode" {
			session.SetSteeringMode(command.Mode)
		} else {
			session.SetFollowUpMode(command.Mode)
		}
		return nil, false, nil
	case "clear_queue":
		steering, followUp := session.ClearQueue()
		return jsonEventObject{{"steering", steering}, {"followUp", followUp}}, true, nil
	case "new_session":
		manager := r.newSessionManager()
		if command.ParentSession != nil && *command.ParentSession != "" {
			parent := *command.ParentSession
			manager.NewSession(&NewSessionOptions{ParentSession: &parent})
		}
		if err := r.installSession(r.ctx, manager); err != nil {
			return nil, false, err
		}
		return jsonEventObject{{"cancelled", false}}, true, nil
	case "switch_session":
		manager, err := OpenSession(*command.SessionPath, "", "")
		if err != nil {
			return nil, false, err
		}
		if err := AssertSessionCwdExists(manager, session.Sessions.GetCwd()); err != nil {
			return nil, false, err
		}
		manager.SetWriteQueue(session.Sessions.GetWriteQueue())
		if err := r.installSession(r.ctx, manager); err != nil {
			return nil, false, err
		}
		return jsonEventObject{{"cancelled", false}}, true, nil
	case "fork":
		position := "before"
		if command.Position != nil && *command.Position != "" {
			position = *command.Position
		}
		if position != "before" && position != "at" {
			return nil, false, fmt.Errorf("position must be before or at")
		}
		manager, selectedText, err := r.forkManager(*command.EntryID, position == "at")
		if err != nil {
			return nil, false, err
		}
		if err := r.installSession(r.ctx, manager); err != nil {
			return nil, false, err
		}
		value := jsonEventObject{}
		if selectedText != nil {
			value.add("text", *selectedText)
		}
		value.add("cancelled", false)
		return value, true, nil
	case "clone":
		leafID := session.Sessions.GetLeafID()
		if leafID == nil {
			return nil, false, fmt.Errorf("Cannot clone session: no current entry selected")
		}
		manager, _, err := r.forkManager(*leafID, true)
		if err != nil {
			return nil, false, err
		}
		if err := r.installSession(r.ctx, manager); err != nil {
			return nil, false, err
		}
		return jsonEventObject{{"cancelled", false}}, true, nil
	case "abort":
		r.settleWorkers()
		return nil, false, nil
	case "compact":
		instructions := ""
		if command.CustomInstructions != nil {
			instructions = *command.CustomInstructions
		}
		result, err := session.CompactSession(r.ctx, instructions)
		if err != nil {
			return nil, false, err
		}
		return rpcCompactionResultValue(result), true, nil
	case "set_auto_compaction":
		session.SetAutoCompactionEnabled(*command.Enabled)
		return nil, false, nil
	case "set_auto_retry":
		// Upstream toggles the persisted setting (settingsManager.setRetryEnabled),
		// which is what the run loop's effective retry reads.
		session.SetRetryEnabled(*command.Enabled)
		return nil, false, nil
	case "abort_retry":
		session.AbortRetry()
		return nil, false, nil
	case "bash":
		exclude := command.ExcludeFromContext != nil && *command.ExcludeFromContext
		options := &ExecuteBashOptions{ExcludeFromContext: exclude}
		if command.ID != nil {
			options.ID = *command.ID
		}
		result, err := session.ExecuteBash(r.ctx, *command.Command, nil, options)
		if err != nil {
			return nil, false, err
		}
		return rpcBashResultValue(result), true, nil
	case "abort_bash":
		session.AbortBash()
		return nil, false, nil
	case "get_session_stats":
		return rpcSessionStatsValue(session.GetSessionStats()), true, nil
	case "export_html":
		if r.exportToHtml == nil {
			return nil, false, fmt.Errorf("HTML export is not available in this build")
		}
		outputPath := ""
		if command.OutputPath != nil {
			outputPath = *command.OutputPath
		}
		path, err := r.exportToHtml(session, outputPath)
		if err != nil {
			return nil, false, err
		}
		return jsonEventObject{{"path", path}}, true, nil
	case "get_last_assistant_text":
		// Upstream builds { text } from string | undefined; JSON.stringify drops
		// the undefined value, so no assistant text omits the field.
		value := jsonEventObject{}
		if text := session.GetLastAssistantText(); text != "" {
			value.add("text", text)
		}
		return value, true, nil
	case "get_fork_messages":
		messages := make([]any, 0)
		for _, message := range session.GetUserMessagesForForking() {
			messages = append(messages, jsonEventObject{{"entryId", message.EntryID}, {"text", message.Text}})
		}
		return jsonEventObject{{"messages", messages}}, true, nil
	case "get_entries":
		entries := session.Sessions.GetEntries()
		if entries == nil {
			entries = []SessionEntry{}
		}
		if command.Since != nil {
			index := -1
			for i := range entries {
				if entries[i].ID == *command.Since {
					index = i
					break
				}
			}
			if index == -1 {
				return nil, false, fmt.Errorf("Entry not found: %s", *command.Since)
			}
			entries = entries[index+1:]
		}
		return jsonEventObject{{"entries", entries}, {"leafId", session.Sessions.GetLeafID()}}, true, nil
	case "get_tree":
		return jsonEventObject{{"tree", rpcTreeValue(session.Sessions.GetTree())}, {"leafId", session.Sessions.GetLeafID()}}, true, nil
	case "set_session_name":
		name := ai.JSTrim(*command.Name)
		if name == "" {
			return nil, false, fmt.Errorf("Session name cannot be empty")
		}
		session.SetSessionName(name)
		return nil, false, nil
	case "get_messages":
		return jsonEventObject{{"messages", jsonMessageValues(session.Messages())}}, true, nil
	case "get_commands":
		commands := make([]any, 0)
		for _, template := range session.PromptTemplates() {
			value := jsonEventObject{{"name", template.Name}}
			if template.Description != "" {
				value.add("description", template.Description)
			}
			value.add("source", "prompt")
			value.add("sourceInfo", rpcSourceInfoValue(template.SourceInfo))
			commands = append(commands, value)
		}
		for _, skill := range session.Skills() {
			value := jsonEventObject{{"name", "skill:" + skill.Name}}
			if skill.Description != "" {
				value.add("description", skill.Description)
			}
			value.add("source", "skill")
			value.add("sourceInfo", rpcSourceInfoValue(skill.SourceInfo))
			commands = append(commands, value)
		}
		return jsonEventObject{{"commands", commands}}, true, nil
	case "get_state":
		model := session.Model()
		if !session.HasModel() {
			model = nil
		}
		thinking := session.ThinkingLevel()
		if thinking == "" {
			thinking = ai.ThinkOff
		}
		return rpcSessionState{
			Model: model, ThinkingLevel: thinking, IsStreaming: session.IsStreaming(),
			IsCompacting: session.IsCompacting(), SteeringMode: session.SteeringMode(),
			FollowUpMode: session.FollowUpMode(), SessionFile: session.SessionFile(),
			SessionID: session.SessionID(), SessionName: session.SessionName(),
			AutoCompactionEnabled: session.AutoCompactionEnabled(), MessageCount: len(session.Messages()),
			PendingMessageCount: session.PendingMessageCount(),
		}, true, nil
	default:
		return nil, false, fmt.Errorf("Unknown command: %s", command.Type)
	}
}

// rpcSourceInfoValue serializes a resource source with upstream key order.
func rpcSourceInfoValue(info SourceInfo) jsonEventObject {
	value := jsonEventObject{
		{"path", info.Path}, {"source", info.Source}, {"scope", info.Scope}, {"origin", info.Origin},
	}
	if info.BaseDir != "" {
		value.add("baseDir", info.BaseDir)
	}
	return value
}

// rpcSessionStatsValue mirrors the camelCase SessionStats wire shape. An empty
// session file and a nil context usage are omitted, as JSON.stringify drops
// the upstream undefined values.
func rpcSessionStatsValue(stats *SessionStats) jsonEventObject {
	value := jsonEventObject{}
	if stats.SessionFile != "" {
		value.add("sessionFile", stats.SessionFile)
	}
	value.add("sessionId", stats.SessionID)
	value.add("userMessages", stats.UserMessages)
	value.add("assistantMessages", stats.AssistantMessages)
	value.add("toolCalls", stats.ToolCalls)
	value.add("toolResults", stats.ToolResults)
	value.add("totalMessages", stats.TotalMessages)
	value.add("tokens", jsonEventObject{
		{"input", stats.Tokens.Input}, {"output", stats.Tokens.Output},
		{"cacheRead", stats.Tokens.CacheRead}, {"cacheWrite", stats.Tokens.CacheWrite},
		{"total", stats.Tokens.Total},
	})
	value.add("cost", stats.Cost)
	if stats.ContextUsage != nil {
		value.add("contextUsage", jsonEventObject{
			{"tokens", stats.ContextUsage.Tokens},
			{"contextWindow", stats.ContextUsage.ContextWindow},
			{"percent", stats.ContextUsage.Percent},
		})
	}
	return value
}

// rpcCompactionResultValue matches the compaction_end result shape.
func rpcCompactionResultValue(result *CompactionResult) jsonEventObject {
	value := jsonEventObject{
		{"summary", result.Summary},
		{"firstKeptEntryId", result.FirstKeptEntryID},
		{"tokensBefore", result.TokensBefore},
	}
	if result.EstimatedTokensAfter != 0 {
		value.add("estimatedTokensAfter", result.EstimatedTokensAfter)
	}
	if result.Usage != nil {
		value.add("usage", *result.Usage)
	}
	if len(result.Details) > 0 {
		value.add("details", result.Details)
	}
	return value
}

// rpcBashResultValue drops exitCode when the process was killed and
// fullOutputPath when there was no truncation (upstream's undefined values).
func rpcBashResultValue(result *BashResult) jsonEventObject {
	value := jsonEventObject{{"output", result.Output}}
	if result.ExitCode != nil {
		value.add("exitCode", *result.ExitCode)
	}
	value.add("cancelled", result.Cancelled)
	value.add("truncated", result.Truncated)
	if result.FullOutputPath != "" {
		value.add("fullOutputPath", result.FullOutputPath)
	}
	return value
}

// rpcTreeValue serializes session tree nodes with upstream key order, omitting
// absent labels.
func rpcTreeValue(nodes []*SessionTreeNode) []any {
	out := make([]any, 0, len(nodes))
	for _, node := range nodes {
		value := jsonEventObject{{"entry", node.Entry}, {"children", rpcTreeValue(node.Children)}}
		if node.HasLabel {
			value.add("label", node.Label)
			if node.LabelTimestamp != "" {
				value.add("labelTimestamp", node.LabelTimestamp)
			}
		}
		out = append(out, value)
	}
	return out
}
