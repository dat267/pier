package rpcclient

// Typed command methods (upstream RpcClient's public surface). Each builds the
// rpc-mode command object, sends it, and decodes the correlated response's
// data. Optional string fields are omitted when empty, matching what
// JSON.stringify does with undefined upstream.

import (
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

func (c *RpcClient) sendDecode(command map[string]any, target any) error {
	response, err := c.send(command)
	if err != nil {
		return err
	}
	return response.decode(target)
}

// Prompt sends a prompt and returns its disposition after acceptance. Use
// OnEvent/PromptAndWait to observe the run.
func (c *RpcClient) Prompt(message string, images []ai.ImageContent, streamingBehavior string) (string, error) {
	command := map[string]any{"type": "prompt", "message": message}
	if images != nil {
		command["images"] = images
	}
	if streamingBehavior != "" {
		command["streamingBehavior"] = streamingBehavior
	}
	var data struct {
		Disposition string `json:"disposition"`
	}
	if err := c.sendDecode(command, &data); err != nil {
		return "", err
	}
	return data.Disposition, nil
}

// Steer queues a steering message and returns its disposition.
func (c *RpcClient) Steer(message string, images []ai.ImageContent) (string, error) {
	return c.queue("steer", message, images)
}

// FollowUp queues a follow-up message and returns its disposition.
func (c *RpcClient) FollowUp(message string, images []ai.ImageContent) (string, error) {
	return c.queue("follow_up", message, images)
}

func (c *RpcClient) queue(kind, message string, images []ai.ImageContent) (string, error) {
	command := map[string]any{"type": kind, "message": message}
	if images != nil {
		command["images"] = images
	}
	var data struct {
		Disposition string `json:"disposition"`
	}
	if err := c.sendDecode(command, &data); err != nil {
		return "", err
	}
	return data.Disposition, nil
}

// Abort cancels the current operation.
func (c *RpcClient) Abort() error {
	return c.sendDecode(map[string]any{"type": "abort"}, nil)
}

// ClearQueue clears queued steering and follow-up messages.
func (c *RpcClient) ClearQueue() (*ClearQueueResult, error) {
	data := &ClearQueueResult{}
	if err := c.sendDecode(map[string]any{"type": "clear_queue"}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// NewSession starts a fresh session, optionally recording a parent session
// path for lineage.
func (c *RpcClient) NewSession(parentSession string) (*SessionResult, error) {
	command := map[string]any{"type": "new_session"}
	if parentSession != "" {
		command["parentSession"] = parentSession
	}
	data := &SessionResult{}
	if err := c.sendDecode(command, data); err != nil {
		return nil, err
	}
	return data, nil
}

// State returns the current session state.
func (c *RpcClient) State() (*SessionState, error) {
	data := &SessionState{}
	if err := c.sendDecode(map[string]any{"type": "get_state"}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// SetModel selects a model by provider and id.
func (c *RpcClient) SetModel(provider, modelID string) (*ai.Model, error) {
	data := &ai.Model{}
	if err := c.sendDecode(map[string]any{"type": "set_model", "provider": provider, "modelId": modelID}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// CycleModel advances the model cycle; nil when there is no cycle.
func (c *RpcClient) CycleModel() (*CycleModelResult, error) {
	var data *CycleModelResult
	if err := c.sendDecode(map[string]any{"type": "cycle_model"}, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// AvailableModels returns the authenticated model snapshot.
func (c *RpcClient) AvailableModels() ([]ModelInfo, error) {
	var data struct {
		Models []ModelInfo `json:"models"`
	}
	if err := c.sendDecode(map[string]any{"type": "get_available_models"}, &data); err != nil {
		return nil, err
	}
	return data.Models, nil
}

// SetThinkingLevel selects a thinking level.
func (c *RpcClient) SetThinkingLevel(level string) error {
	return c.sendDecode(map[string]any{"type": "set_thinking_level", "level": level}, nil)
}

// CycleThinkingLevel advances the thinking level; nil when there is no cycle.
func (c *RpcClient) CycleThinkingLevel() (*string, error) {
	var data *struct {
		Level string `json:"level"`
	}
	if err := c.sendDecode(map[string]any{"type": "cycle_thinking_level"}, &data); err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return &data.Level, nil
}

// AvailableThinkingLevels returns the levels for the current model.
func (c *RpcClient) AvailableThinkingLevels() ([]string, error) {
	var data struct {
		Levels []string `json:"levels"`
	}
	if err := c.sendDecode(map[string]any{"type": "get_available_thinking_levels"}, &data); err != nil {
		return nil, err
	}
	return data.Levels, nil
}

// SetSteeringMode sets the steering queue mode ("all" or "one-at-a-time").
func (c *RpcClient) SetSteeringMode(mode string) error {
	return c.sendDecode(map[string]any{"type": "set_steering_mode", "mode": mode}, nil)
}

// SetFollowUpMode sets the follow-up queue mode ("all" or "one-at-a-time").
func (c *RpcClient) SetFollowUpMode(mode string) error {
	return c.sendDecode(map[string]any{"type": "set_follow_up_mode", "mode": mode}, nil)
}

// Compact summarizes the session context.
func (c *RpcClient) Compact(customInstructions string) (*coding.CompactionResult, error) {
	command := map[string]any{"type": "compact"}
	if customInstructions != "" {
		command["customInstructions"] = customInstructions
	}
	data := &coding.CompactionResult{}
	if err := c.sendDecode(command, data); err != nil {
		return nil, err
	}
	return data, nil
}

// SetAutoCompaction toggles automatic compaction.
func (c *RpcClient) SetAutoCompaction(enabled bool) error {
	return c.sendDecode(map[string]any{"type": "set_auto_compaction", "enabled": enabled}, nil)
}

// SetAutoRetry toggles automatic retry.
func (c *RpcClient) SetAutoRetry(enabled bool) error {
	return c.sendDecode(map[string]any{"type": "set_auto_retry", "enabled": enabled}, nil)
}

// AbortRetry cancels an in-progress retry.
func (c *RpcClient) AbortRetry() error {
	return c.sendDecode(map[string]any{"type": "abort_retry"}, nil)
}

// Bash runs a bash command in the session.
func (c *RpcClient) Bash(command string) (*coding.BashResult, error) {
	data := &coding.BashResult{}
	if err := c.sendDecode(map[string]any{"type": "bash", "command": command}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// AbortBash cancels the running bash command.
func (c *RpcClient) AbortBash() error {
	return c.sendDecode(map[string]any{"type": "abort_bash"}, nil)
}

// SessionStats returns the session counters and token totals.
func (c *RpcClient) SessionStats() (*coding.SessionStats, error) {
	data := &coding.SessionStats{}
	if err := c.sendDecode(map[string]any{"type": "get_session_stats"}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// ExportHTML exports the current session branch to HTML.
func (c *RpcClient) ExportHTML(outputPath string) (*ExportResult, error) {
	command := map[string]any{"type": "export_html"}
	if outputPath != "" {
		command["outputPath"] = outputPath
	}
	data := &ExportResult{}
	if err := c.sendDecode(command, data); err != nil {
		return nil, err
	}
	return data, nil
}

// SwitchSession switches to another session file.
func (c *RpcClient) SwitchSession(sessionPath string) (*SessionResult, error) {
	data := &SessionResult{}
	if err := c.sendDecode(map[string]any{"type": "switch_session", "sessionPath": sessionPath}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Fork branches the session before (or at) an entry.
func (c *RpcClient) Fork(entryID string) (*ForkResult, error) {
	data := &ForkResult{}
	if err := c.sendDecode(map[string]any{"type": "fork", "entryId": entryID}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Clone copies the current branch into a new session.
func (c *RpcClient) Clone() (*SessionResult, error) {
	data := &SessionResult{}
	if err := c.sendDecode(map[string]any{"type": "clone"}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// ForkMessages returns the user messages that can seed a fork.
func (c *RpcClient) ForkMessages() ([]ForkMessage, error) {
	var data struct {
		Messages []ForkMessage `json:"messages"`
	}
	if err := c.sendDecode(map[string]any{"type": "get_fork_messages"}, &data); err != nil {
		return nil, err
	}
	return data.Messages, nil
}

// Entries returns the session entries, optionally after the since entry id.
func (c *RpcClient) Entries(since string) (*EntriesResult, error) {
	command := map[string]any{"type": "get_entries"}
	if since != "" {
		command["since"] = since
	}
	data := &EntriesResult{}
	if err := c.sendDecode(command, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Tree returns the session entry tree.
func (c *RpcClient) Tree() (*TreeResult, error) {
	data := &TreeResult{}
	if err := c.sendDecode(map[string]any{"type": "get_tree"}, data); err != nil {
		return nil, err
	}
	return data, nil
}

// LastAssistantText returns the last assistant text, or nil when there is none.
func (c *RpcClient) LastAssistantText() (*string, error) {
	var data struct {
		Text *string `json:"text"`
	}
	if err := c.sendDecode(map[string]any{"type": "get_last_assistant_text"}, &data); err != nil {
		return nil, err
	}
	return data.Text, nil
}

// SetSessionName sets the session's display name (trimmed; empty fails).
func (c *RpcClient) SetSessionName(name string) error {
	return c.sendDecode(map[string]any{"type": "set_session_name", "name": name}, nil)
}

// Messages returns the session's role-tagged messages as decoded JSON objects,
// since Go has no single AgentMessage union.
func (c *RpcClient) Messages() ([]map[string]any, error) {
	var data struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := c.sendDecode(map[string]any{"type": "get_messages"}, &data); err != nil {
		return nil, err
	}
	return data.Messages, nil
}

// Commands returns prompt templates and skills available for invocation.
func (c *RpcClient) Commands() ([]SlashCommand, error) {
	var data struct {
		Commands []SlashCommand `json:"commands"`
	}
	if err := c.sendDecode(map[string]any{"type": "get_commands"}, &data); err != nil {
		return nil, err
	}
	return data.Commands, nil
}
