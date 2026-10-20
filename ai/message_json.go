package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// ContentList is a JSON array of discriminated content blocks.
type ContentList []Content

// MarshalJSON encodes each block with its `type` discriminator first,
// matching upstream key order.
func (l ContentList) MarshalJSON() ([]byte, error) {
	out := make([]json.RawMessage, 0, len(l))
	for _, c := range l {
		enc, err := marshalContentBlock(c)
		if err != nil {
			return nil, err
		}
		out = append(out, enc)
	}
	return MarshalJSON(out)
}

// UnmarshalJSON decodes blocks by their `type` discriminator.
func (l *ContentList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	blocks := make(ContentList, 0, len(raw))
	for _, r := range raw {
		b, err := unmarshalContentBlock(r)
		if err != nil {
			return err
		}
		blocks = append(blocks, b)
	}
	*l = blocks
	return nil
}

// UserContentList is a JSON array of text/image content blocks.
type UserContentList []UserContent

// MarshalJSON encodes each block with its `type` discriminator.
func (l UserContentList) MarshalJSON() ([]byte, error) {
	out := make([]json.RawMessage, 0, len(l))
	for _, c := range l {
		enc, err := marshalContentBlock(c)
		if err != nil {
			return nil, err
		}
		out = append(out, enc)
	}
	return MarshalJSON(out)
}

// UnmarshalJSON decodes blocks by their `type` discriminator, rejecting
// thinking/toolCall blocks that are not valid user content.
func (l *UserContentList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	blocks := make(UserContentList, 0, len(raw))
	for _, r := range raw {
		b, err := unmarshalContentBlock(r)
		if err != nil {
			return err
		}
		uc, ok := b.(UserContent)
		if !ok {
			return fmt.Errorf("ai: %q content is not valid user content", b.contentKind())
		}
		blocks = append(blocks, uc)
	}
	*l = blocks
	return nil
}

// marshalContentBlock encodes one block with its type discriminator as the
// first key, matching upstream key order.
func marshalContentBlock(c Content) (json.RawMessage, error) {
	var kind ContentKind
	switch v := c.(type) {
	case TextContent:
		kind = v.contentKind()
	case *TextContent:
		kind = v.contentKind()
	case ThinkingContent:
		kind = v.contentKind()
	case *ThinkingContent:
		kind = v.contentKind()
	case ImageContent:
		kind = v.contentKind()
	case *ImageContent:
		kind = v.contentKind()
	case ToolCall:
		kind = v.contentKind()
	case *ToolCall:
		kind = v.contentKind()
	default:
		return nil, fmt.Errorf("ai: unknown content block %T", c)
	}
	enc, err := MarshalJSON(c)
	if err != nil {
		return nil, err
	}
	// Splice the discriminator in as the first key, preserving the struct's
	// field order for the rest (Go maps would sort keys, breaking parity).
	kindEnc, err := MarshalJSON(kind)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(enc)+len(kindEnc)+8)
	out = append(out, '{')
	out = append(out, `"type":`...)
	out = append(out, kindEnc...)
	if len(enc) > 2 { // non-empty object body
		out = append(out, ',')
		out = append(out, enc[1:]...)
	} else {
		out = append(out, '}')
	}
	return out, nil
}

// unmarshalContentBlock decodes one block by its type discriminator.
func unmarshalContentBlock(data json.RawMessage) (Content, error) {
	var probe struct {
		Type ContentKind `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	switch probe.Type {
	case KindText:
		var b TextContent
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case KindThinking:
		var b ThinkingContent
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case KindImage:
		var b ImageContent
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case KindToolCall:
		var b ToolCall
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, fmt.Errorf("ai: unknown content block type %q", probe.Type)
	}
}

// CustomMessage serializes as upstream's flat custom-agent-message union:
// bashExecution, custom, branchSummary and compactionSummary each write their
// own fields next to role and timestamp, not nested under a content member.
// Content holds that flat object (the app factories build it in field order);
// Extra preserves a loaded object verbatim so unknown custom roles round-trip.
func (m *CustomMessage) MarshalJSON() ([]byte, error) {
	if len(m.Extra) > 0 {
		return m.Extra, nil
	}
	if len(m.Content) > 0 {
		if customContentHasRole(m.Content) {
			return m.Content, nil
		}
		// A hand-built message carries only the role fields; splice role first
		// and timestamp last (upstream's custom shape).
		return spliceCustomRole(m.Content, m.Role, m.Timestamp)
	}
	role, err := MarshalJSON(m.Role)
	if err != nil {
		return nil, err
	}
	out := []byte(`{"role":` + string(role))
	if m.Timestamp != 0 {
		out = append(out, `,"timestamp":`...)
		out = append(out, strconv.FormatInt(m.Timestamp, 10)...)
	}
	return append(out, '}'), nil
}

// UnmarshalJSON reads an upstream flat custom-agent message, keeping the whole
// object as both Content (for the role-specific decoders) and Extra (so an
// unknown custom role round-trips verbatim).
func (m *CustomMessage) UnmarshalJSON(data []byte) error {
	var probe struct {
		Role      string `json:"role"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	m.Role = probe.Role
	m.Timestamp = probe.Timestamp
	m.Content = append(json.RawMessage(nil), data...)
	m.Extra = append(json.RawMessage(nil), data...)
	return nil
}

func customContentHasRole(data json.RawMessage) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil {
		return false
	}
	_, ok := object["role"]
	return ok
}

func spliceCustomRole(content json.RawMessage, role string, timestamp int64) ([]byte, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("ai: custom message content must be a JSON object")
	}
	inner := trimmed[1 : len(trimmed)-1]
	roleEnc, err := MarshalJSON(role)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(inner)+len(roleEnc)+48)
	out = append(out, '{')
	out = append(out, `"role":`...)
	out = append(out, roleEnc...)
	if len(bytes.TrimSpace(inner)) > 0 {
		out = append(out, ',')
		out = append(out, inner...)
	}
	if timestamp != 0 {
		out = append(out, `,"timestamp":`...)
		out = append(out, strconv.FormatInt(timestamp, 10)...)
	}
	out = append(out, '}')
	return out, nil
}

// MarshalMessage encodes one message with upstream's role discrimination.
func MarshalMessage(m Message) (json.RawMessage, error) {
	enc, err := MarshalJSON(m)
	if err != nil {
		return nil, err
	}
	// CustomMessage carries its own role field first (unknown roles persist
	// through it); the other message types have no role field, and upstream
	// constructs them as { role, ...fields }. Splice the role in first rather
	// than round-tripping through a map, which would sort the keys and break
	// session-file byte parity.
	switch m.(type) {
	case *CustomMessage:
		return enc, nil
	}
	if len(enc) == 0 || enc[0] != '{' {
		// A nil message marshals as null; there is no object to prepend to.
		return enc, nil
	}
	role, err := MarshalJSON(m.messageRole())
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(enc)+len(role)+10)
	out = append(out, '{')
	out = append(out, `"role":`...)
	out = append(out, role...)
	if len(enc) > 2 { // non-empty object body
		out = append(out, ',')
		out = append(out, enc[1:]...)
	} else {
		out = append(out, '}')
	}
	return out, nil
}

// jsonContentPresent distinguishes absent/null content without copying the
// payload into a RawMessage during the discriminator pass.
type jsonContentPresent bool

func (p *jsonContentPresent) UnmarshalJSON(data []byte) error {
	*p = jsonContentPresent(!bytes.Equal(data, []byte("null")))
	return nil
}

// UnmarshalMessage decodes one message by its role discriminator. Missing/null
// content is normalized as in sessionEntryToContextMessages/transformMessages.
func UnmarshalMessage(data json.RawMessage) (Message, error) {
	var probe struct {
		Role    Role               `json:"role"`
		Content jsonContentPresent `json:"content"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	switch probe.Role {
	case RoleSystem:
		var m SystemMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if !probe.Content {
			m.Content = StringOrBlocks{Text: ""}
		}
		return &m, nil
	case RoleUser:
		var m UserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if !probe.Content {
			m.Content = StringOrBlocks{Blocks: ContentList{}}
		}
		return &m, nil
	case RoleAssistant:
		var m AssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case RoleToolResult:
		var m ToolResultMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		// Unknown roles persist as custom messages (upstream AgentMessage
		// extensibility).
		var m CustomMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("ai: unknown message role %q", probe.Role)
		}
		m.Extra = data
		return &m, nil
	}
}

// MarshalMessages encodes a transcript.
func MarshalMessages(messages []Message) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(messages))
	for _, m := range messages {
		enc, err := MarshalMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, enc)
	}
	return out, nil
}

// UnmarshalMessages decodes a transcript.
func UnmarshalMessages(data []json.RawMessage) ([]Message, error) {
	out := make([]Message, 0, len(data))
	for _, enc := range data {
		m, err := UnmarshalMessage(enc)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
