package ai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Tests for the JSON layer: byte compatibility with pi's on-disk transcript
// format. Upstream shape reference: packages/ai/src/types.ts.

func TestContentBlockRoundTrip(t *testing.T) {
	content := ContentList{
		TextContent{Text: "hello"},
		ThinkingContent{Thinking: "hmm", ThinkingSignature: new("sig"), Redacted: false},
		ImageContent{Data: "aGk=", MimeType: "image/png"},
		ToolCall{ID: "call1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)},
	}
	enc, err := MarshalJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"text","text":"hello"},` +
		`{"type":"thinking","thinking":"hmm","thinkingSignature":"sig"},` +
		`{"type":"image","data":"aGk=","mimeType":"image/png"},` +
		`{"type":"toolCall","id":"call1","name":"read","arguments":{"path":"x"}}]`
	if string(enc) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", enc, want)
	}
	var back ContentList
	if err := json.Unmarshal(enc, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 4 {
		t.Fatalf("decoded %d blocks; want 4", len(back))
	}
	if _, ok := back[0].(TextContent); !ok {
		t.Fatalf("block 0 = %T; want TextContent", back[0])
	}
	if _, ok := back[1].(ThinkingContent); !ok {
		t.Fatalf("block 1 = %T; want ThinkingContent", back[1])
	}
	if _, ok := back[2].(ImageContent); !ok {
		t.Fatalf("block 2 = %T; want ImageContent", back[2])
	}
	if _, ok := back[3].(ToolCall); !ok {
		t.Fatalf("block 3 = %T; want ToolCall", back[3])
	}
}

func TestRedactedThinkingRoundTrip(t *testing.T) {
	enc, err := json.Marshal(ThinkingContent{Thinking: "", ThinkingSignature: new("encrypted"), Redacted: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != `{"thinking":"","thinkingSignature":"encrypted","redacted":true}` {
		t.Fatalf("got %s", enc)
	}
}

func TestSystemMessageSectionOrderPreserved(t *testing.T) {
	raw := `{"content":"base","sections":{"zeta":"Z","alpha":"A","mid":null},"timestamp":0}`
	var msg SystemMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	order := msg.SectionOrder()
	if !reflect.DeepEqual(order, []string{"zeta", "alpha", "mid"}) {
		t.Fatalf("section order = %v; want [zeta alpha mid]", order)
	}
	// Nil section removes; re-encoding preserves declaration order.
	enc, err := MarshalJSON(&msg)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"content":"base","sections":{"zeta":"Z","alpha":"A","mid":null},"timestamp":0}`
	if string(enc) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", enc, want)
	}
}

func TestSystemMessageReplayThenEncode(t *testing.T) {
	// A replayed system message (GetCurrentSystemMessage) encodes with
	// sections first-set order and toolsAdded.
	m := GetCurrentSystemMessage([]Message{
		&SystemMessage{
			Content:   StringOrBlocks{Text: ""},
			Sections:  map[string]*string{},
			Timestamp: 0,
		},
	})
	// Upstream: timestamp ??= 0 makes the timestamp defined, so an empty
	// system message still replays into a message.
	if m == nil {
		t.Fatal("empty-content system message should still replay")
	}
	if m.Content.Text != "" {
		t.Fatalf("content = %q; want empty", m.Content.Text)
	}
	// With no system message at all, the replay is nil.
	if got := GetCurrentSystemMessage([]Message{&UserMessage{Timestamp: 1}}); got != nil {
		t.Fatalf("no system messages should replay nil; got %+v", got)
	}

	head := &SystemMessage{Content: StringOrBlocks{Text: "base"}, Timestamp: 0}
	head.SetSection("rules", new("<rules>old</rules>"))
	head.SetSection("docs", new("<docs>d</docs>"))
	head.ToolsAdded = []Tool{{Name: "t1", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}
	late := &SystemMessage{Content: StringOrBlocks{Text: "updated"}, Timestamp: 2}
	late.SetSection("rules", new("<rules>new</rules>"))
	late.SetSection("docs", nil)

	replayed := GetCurrentSystemMessage([]Message{head, late})
	if replayed == nil {
		t.Fatal("replay should exist")
	}
	if replayed.Content.Text != "base\n\nupdated" {
		t.Fatalf("content = %q", replayed.Content.Text)
	}
	if got := GetSystemMessageText(replayed); got != "base\n\nupdated\n\n<rules>new</rules>" {
		t.Fatalf("text = %q", got)
	}
	// docs was removed, rules updated, order preserved.
	enc, err := MarshalJSON(replayed)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"content":"base\n\nupdated","sections":{"rules":"<rules>new</rules>"},"toolsAdded":[{"name":"t1","description":"d","parameters":{"type":"object"}}],"timestamp":0}`
	if string(enc) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", enc, want)
	}
}

func TestMessageRoleRoundTrip(t *testing.T) {
	messages := []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "prompt"}, Timestamp: 0},
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		&UserMessage{Content: StringOrBlocks{Blocks: ContentList{
			TextContent{Text: "look"},
			ImageContent{Data: "aGk=", MimeType: "image/jpeg"},
		}}, Timestamp: 2},
		&AssistantMessage{
			Content: ContentList{
				ThinkingContent{Thinking: "hmm"},
				TextContent{Text: "answer"},
				ToolCall{ID: "c1", Name: "bash", Arguments: json.RawMessage(`{"cmd":"ls"}`)},
			},
			API:        APIAnthropicMessages,
			Provider:   "anthropic",
			Model:      "claude-opus-4-5",
			StopReason: StopToolUse,
			Usage: Usage{
				Input: 10, Output: 5, CacheRead: 2, CacheWrite: 3,
				TotalTokens: 20,
				Cost:        UsageCost{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.2, Total: 3.3},
			},
			Timestamp: 3,
		},
		&ToolResultMessage{
			ToolCallID: "c1",
			ToolName:   "bash",
			Content:    UserContentList{TextContent{Text: "out"}},
			IsError:    false,
			Timestamp:  4,
		},
	}
	enc, err := MarshalMessages(messages)
	if err != nil {
		t.Fatal(err)
	}
	// Spot-check role discrimination and key names.
	var probe []map[string]json.RawMessage
	for _, e := range enc {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(e, &m); err != nil {
			t.Fatal(err)
		}
		probe = append(probe, m)
	}
	roles := []string{"system", "user", "user", "assistant", "toolResult"}
	for i, want := range roles {
		var role string
		json.Unmarshal(probe[i]["role"], &role)
		if role != want {
			t.Fatalf("message %d role = %q; want %q", i, role, want)
		}
	}
	if _, ok := probe[3]["toolCallId"]; ok {
		t.Fatal("assistant message should not have toolCallId")
	}
	if _, ok := probe[4]["toolCallId"]; !ok {
		t.Fatal("toolResult message should have toolCallId")
	}

	back, err := UnmarshalMessages(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(messages) {
		t.Fatalf("decoded %d; want %d", len(back), len(messages))
	}
	for i := range messages {
		if back[i].messageRole() != messages[i].messageRole() {
			t.Fatalf("message %d role mismatch", i)
		}
	}
	reenc, err := MarshalMessages(back)
	if err != nil {
		t.Fatal(err)
	}
	for i := range enc {
		if string(reenc[i]) != string(enc[i]) {
			t.Fatalf("re-encode mismatch at %d:\n got %s\nwant %s", i, reenc[i], enc[i])
		}
	}
}

func TestUserContentListRejectsToolCall(t *testing.T) {
	raw := `[{"type":"toolCall","id":"1","name":"x","arguments":{}}]`
	var l UserContentList
	if err := json.Unmarshal([]byte(raw), &l); err == nil {
		t.Fatal("expected error for toolCall in user content")
	}
}

func TestToolConstrainedSamplingUnion(t *testing.T) {
	tool := Tool{Name: "t", Description: "d", Parameters: json.RawMessage(`{}`),
		ConstrainedSampling: FalseValue}
	enc, err := MarshalJSON(tool)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != `{"name":"t","description":"d","parameters":{},"constrainedSampling":false}` {
		t.Fatalf("got %s", enc)
	}
	var back Tool
	if err := json.Unmarshal(enc, &back); err != nil {
		t.Fatal(err)
	}
	if !back.ConstrainedSampling.Set || !back.ConstrainedSampling.False {
		t.Fatalf("constrainedSampling = %+v; want explicit false", back.ConstrainedSampling)
	}

	tool2 := Tool{Name: "t", Description: "d", Parameters: json.RawMessage(`{}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true,
			Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "require"}}}
	enc2, _ := MarshalJSON(tool2)
	if string(enc2) != `{"name":"t","description":"d","parameters":{},"constrainedSampling":{"type":"json_schema","strict":"require"}}` {
		t.Fatalf("got %s", enc2)
	}
	var back2 Tool
	if err := json.Unmarshal(enc2, &back2); err != nil {
		t.Fatal(err)
	}
	if back2.ConstrainedSampling.Config == nil || back2.ConstrainedSampling.Config.Strict != "require" {
		t.Fatalf("constrainedSampling = %+v", back2.ConstrainedSampling)
	}
}

func TestNormalizeContext(t *testing.T) {
	// Empty stays empty.
	ctx := NormalizeContext(Context{})
	if len(ctx.Messages) != 0 {
		t.Fatalf("empty context produced %d messages", len(ctx.Messages))
	}
	// Prompt and tools fold into a leading system message with timestamp 0.
	prompt := "p"
	ctx = NormalizeContext(Context{
		SystemPrompt: &prompt,
		Tools:        []Tool{{Name: "t", Description: "d", Parameters: json.RawMessage(`{}`)}},
		Messages:     []Message{&UserMessage{Content: StringOrBlocks{Text: "u"}, Timestamp: 5}},
	})
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages = %d; want 2", len(ctx.Messages))
	}
	head := ctx.Messages[0].(*SystemMessage)
	if head.Content.Text != "p" || head.Timestamp != 0 || len(head.ToolsAdded) != 1 {
		t.Fatalf("head = %+v", head)
	}
}

// Upstream builds every non-custom message as { role, ...fields }, so the role
// key is first. MarshalMessage must not reorder the fields through a map:
// session files and proxy payloads are compared byte for byte.
// The reference builds an assistant message as { role, content, api,
// provider, model, usage, stopReason, timestamp } and appends the stream-set
// fields afterwards: providerThinkingLevel at message start, responseId and
// responseModel from the first chunk, rawStopReason and errorMessage at
// finish, then durationMs when the stream ends (utils/event-stream.ts) and
// thinkingLevel last (agent-loop.ts Object.assign). Persisted sessions and
// message_end/get_messages bytes must keep that order.
func TestMarshalMessageOrdersStreamFieldsLikeTheReference(t *testing.T) {
	providerThinkingLevel := "high"
	responseID, responseModel := "r1", "m-2"
	rawStop := "end_turn"
	errorMessage := "provider said stop"
	duration := int64(12)
	thinkingLevel := ModelThinkingLevel("low")
	message := &AssistantMessage{
		Content:               ContentList{},
		API:                   APIAnthropicMessages,
		Provider:              "anthropic",
		Model:                 "m",
		StopReason:            StopStop,
		Timestamp:             2,
		ProviderThinkingLevel: &providerThinkingLevel,
		ResponseID:            &responseID,
		ResponseModel:         &responseModel,
		RawStopReason:         &rawStop,
		ErrorMessage:          &errorMessage,
		DurationMs:            &duration,
		ThinkingLevel:         &thinkingLevel,
	}
	enc, err := MarshalMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"role":"assistant","content":[],"api":"anthropic-messages","provider":"anthropic","model":"m","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":2,"providerThinkingLevel":"high","responseId":"r1","responseModel":"m-2","rawStopReason":"end_turn","errorMessage":"provider said stop","durationMs":12,"thinkingLevel":"low"}`
	if string(enc) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", enc, want)
	}
}

func TestMarshalMessageKeepsRoleFirst(t *testing.T) {
	cases := []struct {
		name    string
		message Message
		want    string
	}{
		{
			name:    "user",
			message: &UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
			want:    `{"role":"user","content":"hi","timestamp":1}`,
		},
		{
			name: "assistant",
			message: &AssistantMessage{
				Content:    ContentList{},
				API:        APIAnthropicMessages,
				Provider:   "anthropic",
				Model:      "m",
				StopReason: StopStop,
				Timestamp:  2,
			},
			want: `{"role":"assistant","content":[],"api":"anthropic-messages","provider":"anthropic","model":"m","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":2}`,
		},
		{
			name:    "toolResult",
			message: &ToolResultMessage{ToolCallID: "c", ToolName: "read", Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 3},
			want:    `{"role":"toolResult","toolCallId":"c","toolName":"read","content":[{"type":"text","text":"out"}],"isError":false,"timestamp":3}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			enc, err := MarshalMessage(testCase.message)
			if err != nil {
				t.Fatal(err)
			}
			if string(enc) != testCase.want {
				t.Fatalf("marshal:\n got %s\nwant %s", enc, testCase.want)
			}
			// A factory-built custom payload already carries role/timestamp and is
			// emitted verbatim; a hand-built payload without role gets it spliced.
			flat := `{"role":"custom","customType":"t","content":"x","display":false,"timestamp":5}`
			custom, err := MarshalMessage(&CustomMessage{Role: "custom", Content: json.RawMessage(flat), Timestamp: 5})
			if err != nil {
				t.Fatal(err)
			}
			if string(custom) != flat {
				t.Fatalf("custom = %s", custom)
			}
			spliced, err := MarshalMessage(&CustomMessage{Role: "custom", Content: json.RawMessage(`{"customType":"t","content":"x"}`)})
			if err != nil {
				t.Fatal(err)
			}
			// The reference always writes `timestamp`; an absent stamp is NaN and
			// serializes as null (D542).
			if string(spliced) != `{"role":"custom","customType":"t","content":"x","timestamp":null}` {
				t.Fatalf("spliced custom = %s", spliced)
			}
		})
	}

	// A nil message is null, not a malformed object with a spliced role.
	nilEnc, err := MarshalMessage(nil)
	if err != nil || string(nilEnc) != "null" {
		t.Fatalf("nil message = %s (%v)", nilEnc, err)
	}
}

// Reference: pi v1.5.0 (f9a26365) reads session lines with JSON.parse, which
// accepts an unpaired surrogate escape inside a tool definition. json/v2 rejects
// it by default, and Tool has its own UnmarshalJSON, so one such tool took the
// whole system message (and its tool set) down with it (A486, D439).
func TestSystemMessageKeepsToolWithLoneSurrogate(t *testing.T) {
	data := json.RawMessage(`{"role":"system","content":[{"type":"text","text":"sys"}],` +
		`"toolsAdded":[{"name":"t","description":"\ud83d","parameters":{"type":"object"}}]}`)
	var m SystemMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("a tool description with a lone surrogate failed the whole system message: %v", err)
	}
	if len(m.ToolsAdded) != 1 || m.ToolsAdded[0].Name != "t" {
		t.Fatalf("tools added = %d, want the one tool", len(m.ToolsAdded))
	}
}

// The same rule reaches a tool's constrained-sampling config.
func TestToolConstrainedSamplingAcceptsLoneSurrogate(t *testing.T) {
	raw := json.RawMessage(`{"name":"c","description":"x","parameters":{"type":"object"},` +
		`"constrainedSampling":{"type":"json_schema","strict":"prefer"}}`)
	var tool Tool
	if err := json.Unmarshal(raw, &tool); err != nil {
		t.Fatalf("tool with constrained sampling: %v", err)
	}
	var cfg ConstrainedSamplingConfig
	if err := json.Unmarshal([]byte(`{"type":"grammar","variants":{"v":"\ud83d"}}`), &cfg); err != nil {
		t.Fatalf("constrained sampling with a lone surrogate: %v", err)
	}
	if cfg.Variants["v"] == "" {
		t.Fatalf("variant text was dropped: %#v", cfg.Variants)
	}
}

// A custom message always carries `timestamp`; the reference builds it with
// `new Date(stamp).getTime()`, which is NaN for a missing or malformed stamp and
// serializes as null. Pier's entryTimestampMS reports 0 for the same input, so
// the encoder must write null rather than drop the key.
func TestCustomMessageKeepsANullTimestamp(t *testing.T) {
	for _, message := range []*CustomMessage{
		{Role: "branchSummary", Timestamp: 0, Content: []byte(`{"summary":"s","fromId":"f"}`)},
		{Role: "compactionSummary", Timestamp: 0, Content: []byte(`{"summary":"s","tokensBefore":1}`)},
		{Role: "custom", Timestamp: 0, Content: []byte(`{"x":1}`)},
		{Role: "bashExecution", Timestamp: 0, Content: []byte(`{"command":"c"}`)},
		{Role: "custom", Timestamp: 0},
	} {
		enc, err := MarshalMessage(message)
		if err != nil {
			t.Fatalf("%s: %v", message.Role, err)
		}
		if !strings.Contains(string(enc), `"timestamp":null`) {
			t.Errorf("%s = %s, want a null timestamp", message.Role, enc)
		}
	}
	// A real stamp is still a number.
	enc, err := MarshalMessage(&CustomMessage{Role: "branchSummary", Timestamp: 1767225600000, Content: []byte(`{"summary":"s"}`)})
	if err != nil || !strings.Contains(string(enc), `"timestamp":1767225600000`) {
		t.Fatalf("stamped = %s, %v; want the number", enc, err)
	}
}

// types.ts:574-578 declares sections as `Record<string, string | null>` and warns that
// "JSON objects reorder" integer-like names. Pier keeps insertion order separately, so an
// integer-like name must STILL serialize in JSON object order (index keys first, ascending)
// or the bytes differ from the reference.
func TestSystemMessageSectionKeysFollowObjectOrder(t *testing.T) {
	message := &SystemMessage{Content: StringOrBlocks{Text: "x"}}
	value := "v"
	for _, name := range []string{"2", "b", "1"} {
		message.SetSection(name, &value)
	}
	// The declaration order is preserved for replay.
	if got := message.SectionOrder(); strings.Join(got, ",") != "2,b,1" {
		t.Fatalf("SectionOrder = %v, want the declaration order 2,b,1", got)
	}
	encoded, err := MarshalJSON(message)
	if err != nil {
		t.Fatal(err)
	}
	// ...but the wire order is JSON object order, as the reference's doc warns.
	want := `{"content":"x","sections":{"1":"v","2":"v","b":"v"},"timestamp":0}`
	if string(encoded) != want {
		t.Fatalf("encoded = %s\nwant     %s", encoded, want)
	}
}

// An unknown block type is kept verbatim instead of dropping its whole message.
// Upstream loads sessions with a plain JSON.parse and no discriminator check, so
// every block survives there; the port rejected the message, losing its valid
// blocks too. Reachable only from a NEWER pier, since the four known types are
// this project's whole set.
func TestUnknownContentBlockSurvivesRoundTrip(t *testing.T) {
	in := `{"role":"user","content":[{"type":"text","text":"keep me"},{"type":"futureBlock","v":1}],"timestamp":1}`
	message, err := UnmarshalMessage([]byte(in))
	if err != nil {
		t.Fatalf("UnmarshalMessage = %v, want the message kept", err)
	}
	user, ok := message.(*UserMessage)
	if !ok {
		t.Fatalf("message = %T, want *UserMessage", message)
	}
	if len(user.Content.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2 (the known text and the opaque block)", len(user.Content.Blocks))
	}
	if _, ok := user.Content.Blocks[1].(OpaqueContent); !ok {
		t.Fatalf("block 1 = %T, want OpaqueContent", user.Content.Blocks[1])
	}
	if kind := user.Content.Blocks[1].contentKind(); kind != "futureBlock" {
		t.Fatalf("opaque kind = %q, want the original discriminator", kind)
	}
	// The block's own bytes survive: unknown members such as "v" are not dropped.
	encoded, err := MarshalJSON(message)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"content":[{"type":"text","text":"keep me"},{"type":"futureBlock","v":1}],"timestamp":1}`; string(encoded) != want {
		t.Fatalf("MarshalJSON = %s, want %s", encoded, want)
	}
}

// A block with no type at all is still an error: the opaque fallback needs a
// discriminator to report, and a typeless block is malformed rather than newer.
func TestContentBlockWithoutTypeIsRejected(t *testing.T) {
	if _, err := UnmarshalMessage([]byte(`{"role":"user","content":[{"text":"x"}],"timestamp":1}`)); err == nil {
		t.Fatal("a block with no type must be rejected")
	}
}
