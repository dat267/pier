package coding

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream appendCustomMessageEntry stores content/details as their actual
// values; the port marshalled the raw JSON text as a string, and the projection
// (CreateCustomMessage via string(entry.Content)) added another escaping layer.
func TestCustomMessageContentRoundTripsRaw(t *testing.T) {
	persist := false
	m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
	for _, content := range []string{`"hello"`, `[{"type":"text","text":"hi"}]`} {
		id := m.AppendCustomMessageEntry("t", json.RawMessage(content), true, json.RawMessage(`{"k":1}`))
		var entry *SessionEntry
		for i := range m.GetEntries() {
			if m.GetEntries()[i].ID == id {
				entry = &m.GetEntries()[i]
			}
		}
		if entry == nil {
			t.Fatal("entry not found")
		}
		if string(entry.Content) != content {
			t.Fatalf("stored content = %s, want %s", entry.Content, content)
		}
		if string(entry.Details) != `{"k":1}` {
			t.Fatalf("stored details = %s", entry.Details)
		}

		projected := SessionEntryToContextMessages(entry)[0].(*ai.CustomMessage)
		var fields customMessageFields
		if err := json.Unmarshal(projected.Content, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields.Content) != content {
			t.Fatalf("projected content = %s, want %s", fields.Content, content)
		}
		if string(fields.Details) != `{"k":1}` {
			t.Fatalf("projected details = %s", fields.Details)
		}

		// Re-persisting the projection must not add another escaping layer.
		again := m.AppendCustomMessageEntry(fields.CustomType, fields.Content, fields.Display, fields.Details)
		for i := range m.GetEntries() {
			if m.GetEntries()[i].ID == again && string(m.GetEntries()[i].Content) != content {
				t.Fatalf("round-trip content = %s, want %s", m.GetEntries()[i].Content, content)
			}
		}
	}
}

// Upstream estimateTokens sums the system content chars, each section's length,
// and JSON.stringify(toolsAdded).length. The port used the rendered prompt text,
// omitting toolsAdded and counting the "\n\n" separators.
func TestSystemMessageTokenEstimateMatchesUpstream(t *testing.T) {
	section := "de"
	system := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: "abc"}, Timestamp: 0}
	system.SetSection("s", &section)
	system.ToolsAdded = []ai.Tool{{Name: "t", Description: "d", Parameters: json.RawMessage(`{}`)}}

	toolsJSON, err := ai.MarshalJSON(system.ToolsAdded)
	if err != nil {
		t.Fatal(err)
	}
	want := ceilDiv(3+len(section)+JSLength(string(toolsJSON)), 4)
	if got := EstimateAgentMessageTokens(system); got != want {
		t.Fatalf("estimate = %d, want %d (content 3 + section %d + tools %d)", got, want, len(section), JSLength(string(toolsJSON)))
	}
}

// Upstream estimateTokens counts a custom message's content field, not its
// whole serialized body.
func TestCustomMessageTokenEstimateMatchesUpstream(t *testing.T) {
	message := CreateCustomMessage("t", json.RawMessage(`"hello"`), true, nil, 0)
	if got := EstimateAgentMessageTokens(message); got != 2 {
		t.Fatalf("estimate = %d; want 2", got)
	}
	// Block content counts its text.
	message = CreateCustomMessage("t", json.RawMessage(`[{"type":"text","text":"hello"}]`), true, nil, 0)
	if got := EstimateAgentMessageTokens(message); got != 2 {
		t.Fatalf("block estimate = %d; want 2", got)
	}
	// bashExecution counts command + output.
	bash := CreateBashExecutionMessage(strings.Repeat("c", 4), strings.Repeat("o", 4), new(0), false, false, "", false, 0, nil)
	if got := EstimateAgentMessageTokens(bash); got != 2 {
		t.Fatalf("bash estimate = %d; want 2", got)
	}
	// Summaries count the summary text.
	summary := CreateCompactionSummaryMessage(strings.Repeat("s", 8), 0, 0)
	if got := EstimateAgentMessageTokens(summary); got != 2 {
		t.Fatalf("summary estimate = %d; want 2", got)
	}
}

// Upstream custom agent messages are flat: the role-specific fields sit next to
// role and timestamp, not nested under a content member. The port nested them,
// so a session created by pi could not be read back and the on-disk bytes
// differed.
func TestCustomAgentMessageFlatShapes(t *testing.T) {
	exitCode := 3
	cases := []struct {
		name    string
		message *ai.CustomMessage
		want    string
	}{
		{
			name:    "bashExecution",
			message: CreateBashExecutionMessage("ls", "out", &exitCode, false, true, "/tmp/out", true, 7, nil),
			want:    `{"role":"bashExecution","command":"ls","output":"out","exitCode":3,"cancelled":false,"truncated":true,"fullOutputPath":"/tmp/out","timestamp":7,"excludeFromContext":true}`,
		},
		{
			name:    "bashExecution cancelled omits exitCode",
			message: CreateBashExecutionMessage("sleep", "", nil, true, false, "", false, 8, nil),
			want:    `{"role":"bashExecution","command":"sleep","output":"","cancelled":true,"truncated":false,"timestamp":8}`,
		},
		{
			name:    "custom",
			message: CreateCustomMessage("t", json.RawMessage(`"x"`), true, json.RawMessage(`{"k":1}`), 9),
			want:    `{"role":"custom","customType":"t","content":"x","display":true,"details":{"k":1},"timestamp":9}`,
		},
		{
			name:    "compactionSummary",
			message: CreateCompactionSummaryMessage("sum", 10, 11),
			want:    `{"role":"compactionSummary","summary":"sum","tokensBefore":10,"timestamp":11}`,
		},
		{
			name:    "branchSummary",
			message: CreateBranchSummaryMessage("sum", "from", 12),
			want:    `{"role":"branchSummary","summary":"sum","fromId":"from","timestamp":12}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			enc, err := ai.MarshalMessage(testCase.message)
			if err != nil {
				t.Fatal(err)
			}
			if string(enc) != testCase.want {
				t.Fatalf("marshal:\n got %s\nwant %s", enc, testCase.want)
			}
			back, err := ai.UnmarshalMessage(enc)
			if err != nil {
				t.Fatal(err)
			}
			custom, ok := back.(*ai.CustomMessage)
			if !ok {
				t.Fatalf("decoded %T", back)
			}
			if custom.Role != testCase.message.Role {
				t.Fatalf("role = %q, want %q", custom.Role, testCase.message.Role)
			}
			reenc, err := ai.MarshalMessage(custom)
			if err != nil || string(reenc) != testCase.want {
				t.Fatalf("re-marshal = %s (%v), want %s", reenc, err, testCase.want)
			}
		})
	}
}

// A flat upstream bash execution loads and contributes its command/output to
// the model context; the old nested reader dropped it.
func TestFlatBashExecutionLoadsIntoContext(t *testing.T) {
	flat := `{"role":"bashExecution","command":"ls -la","output":"file.txt","cancelled":false,"truncated":false,"timestamp":42}`
	message, err := ai.UnmarshalMessage(json.RawMessage(flat))
	if err != nil {
		t.Fatal(err)
	}
	converted := convertCustomMessage(message.(*ai.CustomMessage))
	if converted == nil {
		t.Fatal("flat bash execution dropped from context")
	}
	user, ok := converted.(*ai.UserMessage)
	if !ok {
		t.Fatalf("converted %T", converted)
	}
	text := ""
	for _, block := range user.Content.Blocks {
		if t, ok := block.(ai.TextContent); ok {
			text += t.Text
		}
	}
	if !strings.Contains(text, "ls -la") || !strings.Contains(text, "file.txt") {
		t.Fatalf("converted text = %q", text)
	}
}
