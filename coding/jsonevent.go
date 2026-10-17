package coding

// Port of v1.3.0 modes/json-event.ts. Both public entry points use the same
// event-shaped objects; wire objects retain JavaScript insertion order.

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

type jsonEventField struct {
	name  string
	value any
}

type jsonEventObject []jsonEventField

func (o jsonEventObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, field := range o {
		if i > 0 {
			buffer.WriteByte(',')
		}
		key, err := ai.MarshalJSON(field.name)
		if err != nil {
			return nil, err
		}
		value, err := ai.MarshalJSON(field.value)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func (o *jsonEventObject) add(name string, value any) {
	*o = append(*o, jsonEventField{name, value})
}

// ToJSONEvent returns the JSON/RPC event shape as ordinary Go values. Prefer
// MarshalJSONSessionEvent for wire output, where object key order matters.
func ToJSONEvent(event *SessionEvent) (map[string]any, error) {
	value, err := jsonEventValue(event)
	if err != nil {
		return nil, err
	}
	plain, err := plainJSONEventValue(value)
	if err != nil {
		return nil, err
	}
	return plain.(map[string]any), nil
}

// MarshalJSONSessionEvent removes cumulative message/partial snapshots from
// message_update and preserves event-specific required fields and key order.
func MarshalJSONSessionEvent(event *SessionEvent) ([]byte, error) {
	value, err := jsonEventValue(event)
	if err != nil {
		return nil, err
	}
	return ai.MarshalJSON(value)
}

func jsonEventValue(event *SessionEvent) (jsonEventObject, error) {
	if event == nil {
		return nil, fmt.Errorf("session event is required")
	}
	value := jsonEventObject{{"type", event.Type}}
	a := event.Agent
	switch event.Type {
	case SessionMessageUpdate:
		if a == nil {
			return nil, fmt.Errorf("message_update event has no agent payload")
		}
		assistant, ok := a.Message.(*ai.AssistantMessage)
		if !ok || assistant == nil {
			return nil, fmt.Errorf("message_update message is not an assistant message")
		}
		delta, err := jsonAssistantEventValue(a.AssistantMessageEvent)
		if err != nil {
			return nil, err
		}
		value.add("usage", assistant.Usage)
		value.add("assistantMessageEvent", delta)
	case SessionAgentEnd:
		if a != nil {
			value.add("messages", jsonMessageValues(a.Messages))
		}
		value.add("willRetry", event.WillRetry)
	case SessionAgentSettled:
		value.add("aborted", event.Aborted)
	case SessionTurnEnd:
		if a != nil {
			if a.Message != nil {
				value.add("message", jsonMessageValue(a.Message))
			}
			value.add("toolResults", jsonMessageValues(a.ToolResults))
		}
	case SessionMessageStart, SessionMessageEnd:
		if a != nil && a.Message != nil {
			value.add("message", jsonMessageValue(a.Message))
		}
	case SessionToolExecutionStart, SessionToolExecutionUpdate, SessionToolExecutionEnd:
		if a != nil {
			value.add("toolCallId", a.ToolCallID)
			value.add("toolName", a.ToolName)
			switch event.Type {
			case SessionToolExecutionStart:
				value.add("args", a.Args)
			case SessionToolExecutionUpdate:
				value.add("args", a.Args)
				value.add("partialResult", jsonToolResultValue(a.PartialResult))
			case SessionToolExecutionEnd:
				value.add("result", jsonToolResultValue(a.Result))
				value.add("isError", a.IsError)
				if a.DurationMs != nil {
					value.add("durationMs", *a.DurationMs)
				}
			}
		}
	case SessionQueueUpdate:
		steering, followUp := event.Steering, event.FollowUp
		if steering == nil {
			steering = []string{}
		}
		if followUp == nil {
			followUp = []string{}
		}
		value.add("steering", steering)
		value.add("followUp", followUp)
	case SessionCompactionStart:
		value.add("reason", event.Reason)
	case SessionCompactionEnd:
		value.add("reason", event.Reason)
		if event.Result != nil {
			result := event.Result
			payload := jsonEventObject{{"summary", result.Summary}, {"firstKeptEntryId", result.FirstKeptEntryID}, {"tokensBefore", result.TokensBefore}}
			if result.EstimatedTokensAfter != 0 {
				payload.add("estimatedTokensAfter", result.EstimatedTokensAfter)
			}
			if result.Usage != nil {
				payload.add("usage", *result.Usage)
			}
			if len(result.Details) > 0 {
				payload.add("details", result.Details)
			}
			value.add("result", payload)
		}
		value.add("aborted", event.Aborted)
		value.add("willRetry", event.WillRetry)
		if event.ErrorMessage != "" {
			value.add("errorMessage", event.ErrorMessage)
		}
	case SessionInfoChanged:
		if event.Name != nil {
			value.add("name", *event.Name)
		}
	case SessionThinkingLevelChanged:
		value.add("level", event.Level)
	case SessionAutoRetryStart, SessionSummarizationRetryScheduled:
		value.add("attempt", event.Attempt)
		value.add("maxAttempts", event.MaxAttempts)
		value.add("delayMs", event.DelayMS)
		value.add("errorMessage", event.ErrorMessage)
	case SessionAutoRetryEnd:
		value.add("success", event.Success)
		value.add("attempt", event.Attempt)
		if event.ErrorMessage != "" {
			value.add("finalError", event.ErrorMessage)
		}
	case SessionEntryAppended:
		if event.Entry != nil {
			if len(event.Entry.Raw()) > 0 {
				value.add("entry", event.Entry.Raw())
			} else {
				value.add("entry", event.Entry)
			}
		}
	case SessionSummarizationRetryAttemptStart:
		value.add("source", event.Source)
		if event.Source == "compaction" {
			value.add("reason", event.Reason)
		}
	case SessionBashExecutionUpdate:
		if event.ID != "" {
			value.add("id", event.ID)
		}
		value.add("delta", event.Delta)
	}
	return value, nil
}

func jsonAssistantEventValue(event *ai.AssistantMessageEvent) (jsonEventObject, error) {
	if event == nil {
		return nil, fmt.Errorf("message_update event has no assistant message event")
	}
	value := jsonEventObject{{"type", event.Type}}
	switch event.Type {
	case ai.EventTextStart, ai.EventTextDelta, ai.EventTextEnd,
		ai.EventThinkingStart, ai.EventThinkingDelta, ai.EventThinkingEnd,
		ai.EventToolcallStart, ai.EventToolcallDelta, ai.EventToolcallEnd:
		value.add("contentIndex", event.ContentIndex)
	}
	switch event.Type {
	case ai.EventTextDelta, ai.EventThinkingDelta, ai.EventToolcallDelta:
		value.add("delta", event.Delta)
	case ai.EventTextEnd, ai.EventThinkingEnd:
		value.add("content", event.Content)
	case ai.EventToolcallEnd:
		if event.ToolCall != nil {
			value.add("toolCall", jsonContentBlock{*event.ToolCall})
		}
	case ai.EventDone:
		value.add("reason", event.Reason)
		value.add("message", jsonMessageValue(event.Message))
	case ai.EventError:
		value.add("reason", event.Reason)
		value.add("error", jsonMessageValue(event.Error))
	}
	if event.Type == ai.EventToolcallStart {
		if event.Partial == nil {
			return nil, fmt.Errorf("toolcall_start event has no partial message")
		}
		if event.ContentIndex < 0 || event.ContentIndex >= len(event.Partial.Content) {
			return nil, fmt.Errorf("toolcall_start content at index %d is not a tool call", event.ContentIndex)
		}
		call, ok := event.Partial.Content[event.ContentIndex].(ai.ToolCall)
		if !ok {
			return nil, fmt.Errorf("toolcall_start content at index %d is not a tool call", event.ContentIndex)
		}
		value.add("id", call.ID)
		value.add("toolName", call.Name)
	}
	return value, nil
}

// ContentList's codec supplies the block discriminator and preserves all
// optional provider fields, including signatures and tool argument errors.
type jsonContentBlock struct{ ai.Content }

func (block jsonContentBlock) MarshalJSON() ([]byte, error) {
	data, err := ai.MarshalJSON(ai.ContentList{block.Content})
	if err != nil {
		return nil, err
	}
	return data[1 : len(data)-1], nil
}

func jsonMessageValues(messages []ai.Message) []any {
	out := make([]any, 0, len(messages))
	for _, message := range messages {
		out = append(out, jsonMessageValue(message))
	}
	return out
}

func jsonMessageValue(message ai.Message) any {
	// Prefix role without round-tripping through a map, which sorts message keys.
	switch m := message.(type) {
	case *ai.AssistantMessage:
		return struct {
			Role string `json:"role"`
			*ai.AssistantMessage
		}{"assistant", m}
	case *ai.UserMessage:
		return struct {
			Role string `json:"role"`
			*ai.UserMessage
		}{"user", m}
	case *ai.ToolResultMessage:
		return struct {
			Role string `json:"role"`
			*ai.ToolResultMessage
		}{"toolResult", m}
	case *ai.SystemMessage:
		// SystemMessage has a custom codec (ordered sections and tool changes).
		return jsonRoleMessage{Role: "system", Message: m}
	case *ai.CustomMessage:
		if len(m.Extra) > 0 {
			return m.Extra
		}
		return m
	default:
		return message
	}
}

type jsonRoleMessage struct {
	Role    string
	Message ai.Message
}

func (m jsonRoleMessage) MarshalJSON() ([]byte, error) {
	body, err := ai.MarshalJSON(m.Message)
	if err != nil {
		return nil, err
	}
	role, err := ai.MarshalJSON(m.Role)
	if err != nil {
		return nil, err
	}
	out := append([]byte(`{"role":`), role...)
	if len(body) > 2 {
		out = append(out, ',')
		out = append(out, body[1:]...)
	} else {
		out = append(out, '}')
	}
	return out, nil
}

func jsonToolResultValue(result agent.AgentToolResult) jsonEventObject {
	value := jsonEventObject{{"content", ai.ContentList(result.Content)}}
	if len(result.Details) > 0 {
		value.add("details", result.Details)
	}
	if result.Usage != nil {
		value.add("usage", *result.Usage)
	}
	if result.IsError {
		value.add("isError", true)
	}
	if result.Terminate {
		value.add("terminate", true)
	}
	return value
}

func plainJSONEventValue(value any) (any, error) {
	switch v := value.(type) {
	case jsonEventObject:
		out := make(map[string]any, len(v))
		for _, field := range v {
			plain, err := plainJSONEventValue(field.value)
			if err != nil {
				return nil, err
			}
			out[field.name] = plain
		}
		return out, nil
	case ai.Usage:
		out := map[string]any{
			"input": v.Input, "output": v.Output, "cacheRead": v.CacheRead, "cacheWrite": v.CacheWrite, "totalTokens": v.TotalTokens,
			"cost": map[string]any{"input": v.Cost.Input, "output": v.Cost.Output, "cacheRead": v.Cost.CacheRead, "cacheWrite": v.Cost.CacheWrite, "total": v.Cost.Total},
		}
		if v.CacheWrite1h != nil {
			out["cacheWrite1h"] = *v.CacheWrite1h
		}
		if v.Reasoning != nil {
			out["reasoning"] = *v.Reasoning
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			plain, err := plainJSONEventValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, plain)
		}
		return out, nil
	case string, bool, int, int64, float64, nil:
		return v, nil
	default:
		data, err := ai.MarshalJSON(v)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var out any
		err = decoder.Decode(&out)
		return out, err
	}
}
