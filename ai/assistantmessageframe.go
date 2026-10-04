package ai

// Port of utils/assistant-message-frame.ts: a compact, replayable encoding of
// assistant-stream progress. Terminal settlement is excluded and persisted
// separately; the encoder turns live events into frames, the reducer replays
// frames back into the assistant message.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf16"
)

// AssistantMessageFrame is one compact progress frame. The union is flattened:
// content carries a block object on the *_start variants and a string on the
// *_end variants.
type AssistantMessageFrame struct {
	Type              string            `json:"type"`
	ContentIndex      *int              `json:"contentIndex,omitempty"`
	Content           json.RawMessage   `json:"content,omitempty"`
	Partial           *AssistantMessage `json:"partial,omitempty"`
	Delta             string            `json:"delta,omitempty"`
	TextSignature     *string           `json:"textSignature,omitempty"`
	ThinkingSignature *string           `json:"thinkingSignature,omitempty"`
	Redacted          *bool             `json:"redacted,omitempty"`
	ToolCall          *ToolCall         `json:"toolCall,omitempty"`
	JSON              string            `json:"json,omitempty"`
	ID                string            `json:"id,omitempty"`
	Name              string            `json:"name,omitempty"`
	Arguments         json.RawMessage   `json:"arguments,omitempty"`
	ThoughtSignature  *string           `json:"thoughtSignature,omitempty"`
	Namespace         *string           `json:"namespace,omitempty"`
}

func intFrameIndex(index int) *int { return &index }

func methodJSON(value any) json.RawMessage {
	encoded, _ := MarshalJSON(value)
	return encoded
}

// frameASCIIToolArguments is JSON.stringify(parseStreamingJson("")): an empty
// parsed object.
const frameEmptyToolArguments = "{}"

func utf16Length(value string) int { return len(utf16.Encode([]rune(value))) }

// utf16Suffix drops the first `units` UTF-16 code units (upstream's
// String.prototype.length/slice arithmetic).
func utf16Suffix(value string, units int) string {
	if units <= 0 {
		return value
	}
	encoded := utf16.Encode([]rune(value))
	if units >= len(encoded) {
		return ""
	}
	return string(utf16.Decode(encoded[units:]))
}

// canonicalArguments parses and re-serializes tool arguments so that both sides
// of the checkpoint comparison use the same key order.
func canonicalArguments(arguments json.RawMessage) (string, error) {
	parsed, ok := decodedJSON(ParseStreamingJSONText(string(arguments)))
	if !ok {
		return frameEmptyToolArguments, nil
	}
	return serializedArguments(methodJSON(parsed))
}

// parseStreamingAny parses a streamed JSON object into a Go value.
func parseStreamingAny(text string) any {
	parsed, _ := decodedJSON(ParseStreamingJSONText(text))
	return parsed
}

func serializedArguments(arguments json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(arguments)) == 0 {
		return "", fmt.Errorf("Tool-call arguments are not JSON-serializable")
	}
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, arguments); err != nil {
		return "", fmt.Errorf("Tool-call arguments are not JSON-serializable")
	}
	return buffer.String(), nil
}

func decodedJSON(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	return value, true
}

// isJSONPrefix reports whether snapshot is a structural prefix of current
// (upstream isJsonPrefix).
func isJSONPrefix(snapshot, current any) bool {
	switch typed := snapshot.(type) {
	case string:
		other, ok := current.(string)
		return ok && strings.HasPrefix(other, typed)
	case []any:
		other, ok := current.([]any)
		if !ok || len(typed) > len(other) {
			return false
		}
		for index := range typed {
			if !isJSONPrefix(typed[index], other[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		other, ok := current.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range typed {
			otherValue, has := other[key]
			if !has || !isJSONPrefix(value, otherValue) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(snapshot, current)
	}
}

// AssistantMessageFrameEncoder encodes one assistant stream (upstream
// AssistantMessageFrameEncoder).
type AssistantMessageFrameEncoder struct {
	started  bool
	terminal bool
	blocks   map[int]*frameEncoderBlock
}

type frameEncoderBlock struct {
	kind              string
	coveredChars      int
	deltaChars        int
	caughtUp          bool
	catchupJSON       string
	snapshotArguments string
}

// NewAssistantMessageFrameEncoder creates an encoder for one stream.
func NewAssistantMessageFrameEncoder() *AssistantMessageFrameEncoder {
	return &AssistantMessageFrameEncoder{blocks: map[int]*frameEncoderBlock{}}
}

func frameContentIndex(index int) error {
	if index < 0 {
		return fmt.Errorf("Invalid assistant message frame contentIndex: %d", index)
	}
	return nil
}

func frameEventBlock(event *AssistantMessageEvent) (Content, error) {
	if err := frameContentIndex(event.ContentIndex); err != nil {
		return nil, err
	}
	if event.Partial == nil || event.ContentIndex >= len(event.Partial.Content) {
		return nil, fmt.Errorf("%s event has no content block at index %d", event.Type, event.ContentIndex)
	}
	return event.Partial.Content[event.ContentIndex], nil
}

// Encode turns one stream event into a frame, or nil when it carries no
// progress (done/error, or an already-seen delta).
func (e *AssistantMessageFrameEncoder) Encode(event *AssistantMessageEvent) (*AssistantMessageFrame, error) {
	if e.terminal {
		return nil, fmt.Errorf("Assistant message event %s follows a terminal event", event.Type)
	}
	switch event.Type {
	case EventStart:
		if e.started {
			return nil, fmt.Errorf("Assistant message stream contains more than one start event")
		}
		e.started = true
		return &AssistantMessageFrame{Type: "start", Partial: cloneStartMessage(event.Partial)}, nil
	case EventDone:
		if !e.started {
			return nil, fmt.Errorf("Assistant message done event appears before start")
		}
		e.terminal = true
		return nil, nil
	case EventError:
		e.terminal = true
		return nil, nil
	}
	if !e.started {
		return nil, fmt.Errorf("Assistant message %s event appears before start", event.Type)
	}

	switch event.Type {
	case EventTextStart:
		block, err := frameEventBlock(event)
		if err != nil {
			return nil, err
		}
		content, ok := block.(TextContent)
		if !ok {
			return nil, fmt.Errorf("text_start event points to %s block at index %d", contentKindOf(block), event.ContentIndex)
		}
		e.startBlock(event.ContentIndex, &frameEncoderBlock{kind: "text", coveredChars: utf16Length(content.Text)})
		return &AssistantMessageFrame{Type: "text_start", ContentIndex: intFrameIndex(event.ContentIndex), Content: methodJSON(cloneTextContent(content))}, nil
	case EventTextDelta:
		return e.encodeTextDelta(event.ContentIndex, event.Delta, "text")
	case EventTextEnd:
		block, err := frameEventBlock(event)
		if err != nil {
			return nil, err
		}
		content, ok := block.(TextContent)
		if !ok {
			return nil, fmt.Errorf("text_end event points to %s block at index %d", contentKindOf(block), event.ContentIndex)
		}
		e.endBlock(event.ContentIndex, "text")
		return &AssistantMessageFrame{Type: "text_end", ContentIndex: intFrameIndex(event.ContentIndex), Content: methodJSON(event.Content), TextSignature: content.TextSignature}, nil
	case EventThinkingStart:
		block, err := frameEventBlock(event)
		if err != nil {
			return nil, err
		}
		content, ok := block.(ThinkingContent)
		if !ok {
			return nil, fmt.Errorf("thinking_start event points to %s block at index %d", contentKindOf(block), event.ContentIndex)
		}
		e.startBlock(event.ContentIndex, &frameEncoderBlock{kind: "thinking", coveredChars: utf16Length(content.Thinking)})
		return &AssistantMessageFrame{Type: "thinking_start", ContentIndex: intFrameIndex(event.ContentIndex), Content: methodJSON(cloneThinkingContent(content))}, nil
	case EventThinkingDelta:
		return e.encodeTextDelta(event.ContentIndex, event.Delta, "thinking")
	case EventThinkingEnd:
		block, err := frameEventBlock(event)
		if err != nil {
			return nil, err
		}
		content, ok := block.(ThinkingContent)
		if !ok {
			return nil, fmt.Errorf("thinking_end event points to %s block at index %d", contentKindOf(block), event.ContentIndex)
		}
		e.endBlock(event.ContentIndex, "thinking")
		var redacted *bool
		if content.Redacted {
			value := true
			redacted = &value
		}
		return &AssistantMessageFrame{Type: "thinking_end", ContentIndex: intFrameIndex(event.ContentIndex), Content: methodJSON(event.Content), ThinkingSignature: content.ThinkingSignature, Redacted: redacted}, nil
	case EventToolcallStart:
		block, err := frameEventBlock(event)
		if err != nil {
			return nil, err
		}
		content, ok := block.(ToolCall)
		if !ok {
			return nil, fmt.Errorf("toolcall_start event points to %s block at index %d", contentKindOf(block), event.ContentIndex)
		}
		snapshot, err := canonicalArguments(content.Arguments)
		if err != nil {
			return nil, err
		}
		caughtUp := snapshot == frameEmptyToolArguments
		e.startBlock(event.ContentIndex, &frameEncoderBlock{
			kind: "toolCall", caughtUp: caughtUp, snapshotArguments: chooseString(caughtUp, "", snapshot),
		})
		cloned := cloneFrameToolCall(content)
		return &AssistantMessageFrame{Type: "toolcall_start", ContentIndex: intFrameIndex(event.ContentIndex), ToolCall: &cloned}, nil
	case EventToolcallDelta:
		state, err := e.block(event.ContentIndex, "toolCall")
		if err != nil {
			return nil, err
		}
		if state.caughtUp {
			if event.Delta == "" {
				return nil, nil
			}
			return &AssistantMessageFrame{Type: "toolcall_delta", ContentIndex: intFrameIndex(event.ContentIndex), Delta: event.Delta}, nil
		}
		state.catchupJSON += event.Delta
		argumentsValue, _ := decodedJSON(methodJSON(ParseStreamingJSONText(state.catchupJSON)))
		canonicalValue, _ := serializedArguments(methodJSON(argumentsValue))
		if canonicalValue != state.snapshotArguments {
			if !isJSONPrefix(parseStreamingAny(state.snapshotArguments), argumentsValue) {
				return nil, nil
			}
		}
		state.caughtUp = true
		state.snapshotArguments = ""
		jsonText := state.catchupJSON
		state.catchupJSON = ""
		if jsonText == "" {
			return nil, nil
		}
		return &AssistantMessageFrame{Type: "toolcall_checkpoint", ContentIndex: intFrameIndex(event.ContentIndex), JSON: jsonText}, nil
	case EventToolcallEnd:
		block, err := frameEventBlock(event)
		if err != nil {
			return nil, err
		}
		if _, ok := block.(ToolCall); !ok {
			return nil, fmt.Errorf("toolcall_end event points to %s block at index %d", contentKindOf(block), event.ContentIndex)
		}
		if event.ToolCall == nil {
			return nil, fmt.Errorf("toolcall_end event has invalid tool call at index %d", event.ContentIndex)
		}
		e.endBlock(event.ContentIndex, "toolCall")
		arguments := append(json.RawMessage(nil), event.ToolCall.Arguments...)
		return &AssistantMessageFrame{
			Type: "toolcall_end", ContentIndex: intFrameIndex(event.ContentIndex),
			ID: event.ToolCall.ID, Name: event.ToolCall.Name, Arguments: arguments,
			ThoughtSignature: event.ToolCall.ThoughtSignature, Namespace: event.ToolCall.Namespace,
		}, nil
	}
	return nil, nil
}

func chooseString(condition bool, whenTrue, whenFalse string) string {
	if condition {
		return whenTrue
	}
	return whenFalse
}

func contentKindOf(block Content) string {
	switch block.(type) {
	case TextContent:
		return "text"
	case ThinkingContent:
		return "thinking"
	case ToolCall:
		return "toolCall"
	}
	return "unknown"
}

func (e *AssistantMessageFrameEncoder) startBlock(index int, state *frameEncoderBlock) error {
	if err := frameContentIndex(index); err != nil {
		return err
	}
	if _, exists := e.blocks[index]; exists {
		return fmt.Errorf("Assistant message block %d starts more than once", index)
	}
	e.blocks[index] = state
	return nil
}

func (e *AssistantMessageFrameEncoder) block(index int, kind string) (*frameEncoderBlock, error) {
	if err := frameContentIndex(index); err != nil {
		return nil, err
	}
	state, exists := e.blocks[index]
	if !exists {
		return nil, fmt.Errorf("Assistant message %s block %d has not started", kind, index)
	}
	if state.kind != kind {
		return nil, fmt.Errorf("Assistant message block %d is %s, not %s", index, state.kind, kind)
	}
	return state, nil
}

func (e *AssistantMessageFrameEncoder) endBlock(index int, kind string) error {
	if _, err := e.block(index, kind); err != nil {
		return err
	}
	delete(e.blocks, index)
	return nil
}

func (e *AssistantMessageFrameEncoder) encodeTextDelta(index int, delta string, kind string) (*AssistantMessageFrame, error) {
	state, err := e.block(index, kind)
	if err != nil {
		return nil, err
	}
	deltaStart := state.deltaChars
	state.deltaChars += utf16Length(delta)
	covered := state.coveredChars - deltaStart
	if covered < 0 {
		covered = 0
	}
	if covered >= utf16Length(delta) {
		return nil, nil
	}
	uncovered := delta
	if covered > 0 {
		uncovered = utf16Suffix(delta, covered)
	}
	return &AssistantMessageFrame{Type: kind + "_delta", ContentIndex: intFrameIndex(index), Delta: uncovered}, nil
}

func cloneTextContent(content TextContent) TextContent {
	out := TextContent{Text: content.Text}
	if content.TextSignature != nil {
		value := *content.TextSignature
		out.TextSignature = &value
	}
	return out
}

func cloneThinkingContent(content ThinkingContent) ThinkingContent {
	out := ThinkingContent{Thinking: content.Thinking, Redacted: content.Redacted}
	if content.ThinkingSignature != nil {
		value := *content.ThinkingSignature
		out.ThinkingSignature = &value
	}
	return out
}

func cloneFrameToolCall(call ToolCall) ToolCall {
	out := ToolCall{ID: call.ID, Name: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...)}
	if call.ThoughtSignature != nil {
		value := *call.ThoughtSignature
		out.ThoughtSignature = &value
	}
	if call.Namespace != nil {
		value := *call.Namespace
		out.Namespace = &value
	}
	return out
}

func cloneFrameUsage(usage Usage) Usage {
	out := usage
	if usage.Reasoning != nil {
		value := *usage.Reasoning
		out.Reasoning = &value
	}
	if usage.CacheWrite1h != nil {
		value := *usage.CacheWrite1h
		out.CacheWrite1h = &value
	}
	return out
}

func cloneStartMessage(message *AssistantMessage) *AssistantMessage {
	if message == nil {
		return nil
	}
	return &AssistantMessage{
		Content:               ContentList{},
		API:                   message.API,
		Provider:              message.Provider,
		Model:                 message.Model,
		ResponseModel:         message.ResponseModel,
		ResponseID:            message.ResponseID,
		ProviderThinkingLevel: message.ProviderThinkingLevel,
		Diagnostics:           append([]AssistantMessageDiagnostic(nil), message.Diagnostics...),
		Usage:                 cloneFrameUsage(message.Usage),
		StopReason:            StopPending,
		Timestamp:             message.Timestamp,
	}
}

// reducerBlockState tracks one block while replaying frames.
type reducerBlockState struct {
	kind  string
	ended bool
	json  string
}

// ReduceAssistantMessageFrames replays frames without mutating them, returning
// nil when the list contains no start frame (upstream
// reduceAssistantMessageFrames).
func ReduceAssistantMessageFrames(frames []AssistantMessageFrame) (*AssistantMessage, error) {
	var message *AssistantMessage
	states := map[int]*reducerBlockState{}
	frameBeforeStart := ""
	for index := range frames {
		frame := &frames[index]
		if frame.Type == "start" {
			if message != nil {
				return nil, fmt.Errorf("Assistant message frame sequence contains more than one start frame")
			}
			if frameBeforeStart != "" {
				return nil, fmt.Errorf("%s frame appears before the start frame", frameBeforeStart)
			}
			message = cloneStartMessage(frame.Partial)
			continue
		}
		if message == nil {
			if frameBeforeStart == "" {
				frameBeforeStart = frame.Type
			}
			continue
		}
		if err := reduceAssistantFrame(frame, message, states); err != nil {
			return nil, err
		}
	}
	if message == nil {
		return nil, nil
	}
	for contentIndex, state := range states {
		if state.kind != "toolCall" || state.ended || state.json == "" {
			continue
		}
		block, ok := message.Content[contentIndex].(ToolCall)
		if !ok {
			return nil, fmt.Errorf("Unreachable tool-call frame state")
		}
		block.Arguments = ParseStreamingJSONText(state.json)
		message.Content[contentIndex] = block
	}
	return message, nil
}

func reduceAppendBlock(message *AssistantMessage, states map[int]*reducerBlockState, contentIndex int, block Content, state *reducerBlockState) error {
	if err := frameContentIndex(contentIndex); err != nil {
		return err
	}
	if contentIndex != len(message.Content) {
		reason := "would leave a gap"
		if contentIndex < len(message.Content) {
			reason = "already exists"
		}
		return fmt.Errorf("Cannot start assistant message block at index %d: %s", contentIndex, reason)
	}
	message.Content = append(message.Content, cloneFrameContent(block))
	states[contentIndex] = state
	return nil
}

func cloneFrameContent(block Content) Content {
	switch typed := block.(type) {
	case TextContent:
		return cloneTextContent(typed)
	case ThinkingContent:
		return cloneThinkingContent(typed)
	case ToolCall:
		return cloneFrameToolCall(typed)
	}
	return block
}

func reduceActiveBlock(message *AssistantMessage, states map[int]*reducerBlockState, contentIndex int, expectedKind string, frameType string) (Content, *reducerBlockState, error) {
	if err := frameContentIndex(contentIndex); err != nil {
		return nil, nil, err
	}
	state := states[contentIndex]
	if state == nil || contentIndex >= len(message.Content) {
		return nil, nil, fmt.Errorf("%s frame has no started block at index %d", frameType, contentIndex)
	}
	block := message.Content[contentIndex]
	if state.kind != expectedKind || contentKindOf(block) != expectedKind {
		return nil, nil, fmt.Errorf("%s frame expected %s block at index %d, found %s", frameType, expectedKind, contentIndex, contentKindOf(block))
	}
	if state.ended {
		return nil, nil, fmt.Errorf("%s frame follows the end of block at index %d", frameType, contentIndex)
	}
	return block, state, nil
}

func reduceAssistantFrame(frame *AssistantMessageFrame, message *AssistantMessage, states map[int]*reducerBlockState) error {
	index := 0
	if frame.ContentIndex != nil {
		index = *frame.ContentIndex
	}
	switch frame.Type {
	case "text_start":
		var content TextContent
		if err := json.Unmarshal(frame.Content, &content); err != nil {
			return err
		}
		return reduceAppendBlock(message, states, index, content, &reducerBlockState{kind: "text"})
	case "text_delta":
		block, _, err := reduceActiveBlock(message, states, index, "text", frame.Type)
		if err != nil {
			return err
		}
		text := block.(TextContent)
		text.Text += frame.Delta
		message.Content[index] = text
		return nil
	case "text_end":
		block, state, err := reduceActiveBlock(message, states, index, "text", frame.Type)
		if err != nil {
			return err
		}
		text := block.(TextContent)
		_ = json.Unmarshal(frame.Content, &text.Text)
		text.TextSignature = frame.TextSignature
		message.Content[index] = text
		state.ended = true
		return nil
	case "thinking_start":
		var content ThinkingContent
		if err := json.Unmarshal(frame.Content, &content); err != nil {
			return err
		}
		return reduceAppendBlock(message, states, index, content, &reducerBlockState{kind: "thinking"})
	case "thinking_delta":
		block, _, err := reduceActiveBlock(message, states, index, "thinking", frame.Type)
		if err != nil {
			return err
		}
		thinking := block.(ThinkingContent)
		thinking.Thinking += frame.Delta
		message.Content[index] = thinking
		return nil
	case "thinking_end":
		block, state, err := reduceActiveBlock(message, states, index, "thinking", frame.Type)
		if err != nil {
			return err
		}
		thinking := block.(ThinkingContent)
		_ = json.Unmarshal(frame.Content, &thinking.Thinking)
		thinking.ThinkingSignature = frame.ThinkingSignature
		thinking.Redacted = frame.Redacted != nil && *frame.Redacted
		message.Content[index] = thinking
		state.ended = true
		return nil
	case "toolcall_start":
		if frame.ToolCall == nil {
			return fmt.Errorf("toolcall_start frame has no tool call")
		}
		return reduceAppendBlock(message, states, index, cloneFrameToolCall(*frame.ToolCall), &reducerBlockState{kind: "toolCall"})
	case "toolcall_checkpoint":
		block, state, err := reduceActiveBlock(message, states, index, "toolCall", frame.Type)
		if err != nil {
			return err
		}
		call := block.(ToolCall)
		state.json = frame.JSON
		call.Arguments = ParseStreamingJSONText(frame.JSON)
		message.Content[index] = call
		return nil
	case "toolcall_delta":
		_, state, err := reduceActiveBlock(message, states, index, "toolCall", frame.Type)
		if err != nil {
			return err
		}
		state.json += frame.Delta
		return nil
	case "toolcall_end":
		block, state, err := reduceActiveBlock(message, states, index, "toolCall", frame.Type)
		if err != nil {
			return err
		}
		call := block.(ToolCall)
		call.ID = frame.ID
		call.Name = frame.Name
		call.Arguments = append(json.RawMessage(nil), frame.Arguments...)
		call.ThoughtSignature = frame.ThoughtSignature
		call.Namespace = frame.Namespace
		message.Content[index] = call
		state.ended = true
		return nil
	}
	return nil
}
