package coding

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of core/messages.ts convertToLlm and the custom AgentMessage shapes
// of the coding agent (bashExecution, custom, branchSummary,
// compactionSummary).

// Custom role names (core/messages.ts).
const (
	RoleBashExecution     = "bashExecution"
	RoleCustom            = "custom"
	RoleBranchSummary     = "branchSummary"
	RoleCompactionSummary = "compactionSummary"
)

// CreateBashExecutionMessage builds the `!` bash-execution message.
func CreateBashExecutionMessage(command, output string, exitCode int, cancelled, truncated bool, fullOutputPath string, excludeFromContext bool, timestamp int64) *ai.CustomMessage {
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}
	raw, _ := ai.MarshalJSON(map[string]any{
		"role": RoleBashExecution, "command": command, "output": output,
		"exitCode": exitCode, "cancelled": cancelled, "truncated": truncated,
		"fullOutputPath": fullOutputPath, "timestamp": timestamp,
		"excludeFromContext": excludeFromContext,
	})
	return &ai.CustomMessage{Role: RoleBashExecution, Content: raw, Timestamp: timestamp}
}

// bashExecutionFields decodes a bashExecution custom message.
type bashExecutionFields struct {
	Command            string  `json:"command"`
	Output             string  `json:"output"`
	ExitCode           *int    `json:"exitCode"`
	Cancelled          bool    `json:"cancelled"`
	Truncated          bool    `json:"truncated"`
	FullOutputPath     *string `json:"fullOutputPath"`
	ExcludeFromContext bool    `json:"excludeFromContext"`
}

// BashExecutionToText renders the message for the LLM (port of
// bashExecutionToText).
func BashExecutionToText(msg *bashExecutionFields) string {
	var text strings.Builder
	text.WriteString("Ran `" + msg.Command + "`\n")
	if msg.Output != "" {
		text.WriteString("```\n" + msg.Output + "\n```")
	} else {
		text.WriteString("(no output)")
	}
	if msg.Cancelled {
		text.WriteString("\n\n(command cancelled)")
	} else if msg.ExitCode != nil && *msg.ExitCode != 0 {
		text.WriteString(fmt.Sprintf("\n\nCommand exited with code %d", *msg.ExitCode))
	}
	if msg.Truncated && msg.FullOutputPath != nil {
		text.WriteString(fmt.Sprintf("\n\n[Output truncated. Full output: %s]", *msg.FullOutputPath))
	}
	return text.String()
}

// customMessageFields decodes a custom message.
type customMessageFields struct {
	CustomType string          `json:"customType"`
	Content    json.RawMessage `json:"content"`
	Display    bool            `json:"display"`
	Details    json.RawMessage `json:"details"`
}

// branchSummaryFields decodes a branchSummary message.
type branchSummaryFields struct {
	Summary string  `json:"summary"`
	FromID  *string `json:"fromId"`
}

// compactionSummaryFields decodes a compactionSummary message.
type compactionSummaryFields struct {
	Summary      string `json:"summary"`
	TokensBefore int64  `json:"tokensBefore"`
}

// ConvertToLlm maps coding-agent AgentMessages to LLM messages
// (port of messages.ts convertToLlm).
func ConvertToLlm(messages []ai.Message) []ai.Message {
	var out []ai.Message
	// A custom message appended while a tool call is unanswered (a goal
	// lifecycle event fired during tool execution) lands between the assistant
	// tool call and its result. Providers require the result to follow the call
	// directly, so hold the converted note back until the pending calls are
	// answered, then emit it after them.
	pendingToolCalls := map[string]bool{}
	var heldCustom []ai.Message
	flushHeld := func() {
		if len(pendingToolCalls) == 0 && len(heldCustom) > 0 {
			out = append(out, heldCustom...)
			heldCustom = heldCustom[:0]
		}
	}
	for _, m := range messages {
		switch msg := m.(type) {
		case *ai.SystemMessage, *ai.UserMessage:
			out = append(out, m)
		case *ai.AssistantMessage:
			pendingToolCalls = map[string]bool{}
			for _, block := range msg.Content {
				if call, ok := block.(ai.ToolCall); ok {
					pendingToolCalls[call.ID] = true
				}
			}
			out = append(out, m)
		case *ai.ToolResultMessage:
			delete(pendingToolCalls, msg.ToolCallID)
			out = append(out, m)
			flushHeld()
		case *ai.CustomMessage:
			converted := convertCustomMessage(msg)
			if converted == nil {
				continue
			}
			if len(pendingToolCalls) > 0 {
				heldCustom = append(heldCustom, converted)
				continue
			}
			out = append(out, converted)
		default:
			continue
		}
	}
	out = append(out, heldCustom...)
	return out
}

// convertCustomMessage projects a custom AgentMessage onto an LLM message, or
// nil when the role is excluded or unknown.
func convertCustomMessage(msg *ai.CustomMessage) ai.Message {
	switch msg.Role {
	case RoleBashExecution:
		var fields bashExecutionFields
		if json.Unmarshal(msg.Content, &fields) != nil {
			return nil
		}
		if fields.ExcludeFromContext {
			return nil // !! prefix: excluded from context
		}
		return &ai.UserMessage{
			Content:   ai.StringOrBlocks{Blocks: ai.ContentList{ai.TextContent{Text: BashExecutionToText(&fields)}}},
			Timestamp: msg.Timestamp,
		}
	case RoleCustom:
		var fields customMessageFields
		if json.Unmarshal(msg.Content, &fields) != nil {
			return nil
		}
		return &ai.UserMessage{
			Content:   customContentToBlocks(fields.Content),
			Timestamp: msg.Timestamp,
		}
	case RoleBranchSummary:
		var fields branchSummaryFields
		if json.Unmarshal(msg.Content, &fields) != nil {
			return nil
		}
		return &ai.UserMessage{
			Content:   ai.StringOrBlocks{Blocks: ai.ContentList{ai.TextContent{Text: BranchSummaryPrefix + fields.Summary + BranchSummarySuffix}}},
			Timestamp: msg.Timestamp,
		}
	case RoleCompactionSummary:
		var fields compactionSummaryFields
		if json.Unmarshal(msg.Content, &fields) != nil {
			return nil
		}
		return &ai.UserMessage{
			Content: ai.StringOrBlocks{Blocks: ai.ContentList{ai.TextContent{
				Text: CompactionSummaryPrefix + fields.Summary + CompactionSummarySuffix,
			}}},
			Timestamp: msg.Timestamp,
		}
	default:
		// Unknown custom role: unconvertible, filtered out.
		return nil
	}
}

// customContentToBlocks normalizes string|blocks content to blocks.
func customContentToBlocks(content json.RawMessage) ai.StringOrBlocks {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return ai.StringOrBlocks{Blocks: ai.ContentList{ai.TextContent{Text: text}}}
	}
	var list ai.ContentList
	if err := json.Unmarshal(content, &list); err == nil {
		return ai.StringOrBlocks{Blocks: list}
	}
	return ai.StringOrBlocks{Blocks: ai.ContentList{ai.TextContent{Text: string(content)}}}
}
