package coding

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
)

// RunPrintMode drives an AgentSession to completion headlessly: text mode
// prints the last assistant message's text; json mode streams the session
// header plus one JSON object per session event, with the cumulative partial
// snapshots stripped from message_update (upstream toJsonEvent).

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = saved }()
	run()
	_ = writer.Close()
	out := make([]byte, 64*1024)
	n, _ := reader.Read(out)
	_ = reader.Close()
	return string(out[:n])
}

func newPrintSession(t *testing.T, responses ...*ai.AssistantMessage) (*AgentSession, *SessionManager) {
	t.Helper()
	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	session, err := NewAgentSession(&SessionConfig{
		Cwd:      dir,
		Model:    &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000},
		StreamFn: mockSessionStreamFn(responses...),
		Sessions: sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session, sessions
}

func TestPrintModeReportsPersistenceFailure(t *testing.T) {
	for _, mode := range []CLIMode{CLIModeText, CLIModeJSON} {
		for _, queued := range []bool{false, true} {
			name := string(mode) + "/" + map[bool]string{false: "sync", true: "queued"}[queued]
			t.Run(name, func(t *testing.T) {
				session, sessions := newPrintSession(t, createAssistantMessageT("reply"))
				if queued {
					queue := offloop.New()
					t.Cleanup(queue.Stop)
					sessions.SetWriteQueue(queue)
				}
				if err := os.Mkdir(sessions.GetSessionFile(), 0o755); err != nil {
					t.Fatal(err)
				}
				var exitCode int
				var output string
				diagnostics := captureStdout(t, func() {
					// The outer pipe captures stderr; the inner pipe keeps stdout
					// separate so JSON consumers never see diagnostic text.
					stderr := os.Stderr
					os.Stderr = os.Stdout
					defer func() { os.Stderr = stderr }()
					output = captureStdout(t, func() {
						exitCode = RunPrintMode(session, sessions, PrintModeOptions{Mode: mode, InitialMessage: "save this"})
					})
				})
				if exitCode != 1 {
					t.Fatalf("exit code = %d, want 1 for persistence failure", exitCode)
				}
				if !strings.Contains(diagnostics, "Session rewrite failed") {
					t.Fatalf("persistence diagnostic missing from stderr: %q", diagnostics)
				}
				if strings.Contains(output, "Session rewrite failed") {
					t.Fatal("persistence diagnostic leaked into stdout")
				}
				if mode == CLIModeJSON {
					for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
						if !json.Valid([]byte(line)) {
							t.Fatalf("invalid JSON stdout line: %q", line)
						}
					}
				}
			})
		}
	}
}

// TestPrintModeInitialImagesOnlyAttachToFirstPrompt follows
// packages/coding-agent/src/modes/print-mode.ts at v1.2.0.
func TestPrintModeInitialImagesOnlyAttachToFirstPrompt(t *testing.T) {
	for _, mode := range []CLIMode{CLIModeText, CLIModeJSON} {
		t.Run(string(mode), func(t *testing.T) {
			session, sessions := newPrintSession(t, createAssistantMessageT("first"), createAssistantMessageT("second"))
			image := ai.ImageContent{Data: base64.StdEncoding.EncodeToString(solidPNG(t, 1, 1)), MimeType: "image/png"}
			// Decode options so this regression also runs against the old API,
			// which silently discarded InitialImages before sending the prompt.
			encoded, err := ai.MarshalJSON(map[string]any{
				"Mode": mode, "InitialMessage": "describe", "InitialImages": []ai.ImageContent{image}, "Messages": []string{"continue"},
			})
			if err != nil {
				t.Fatal(err)
			}
			var options PrintModeOptions
			if err := json.Unmarshal(encoded, &options); err != nil {
				t.Fatal(err)
			}
			var code int
			captureStdout(t, func() { code = RunPrintMode(session, sessions, options) })
			if code != 0 {
				t.Fatalf("print exit = %d", code)
			}
			var prompts []*ai.UserMessage
			for _, message := range session.Agent.State().Messages {
				if user, ok := message.(*ai.UserMessage); ok {
					prompts = append(prompts, user)
				}
			}
			if len(prompts) != 2 {
				t.Fatalf("user prompts = %d, want 2", len(prompts))
			}
			if len(prompts[0].Content.Blocks) != 2 || prompts[0].Content.Blocks[1] != image {
				t.Errorf("first prompt content = %#v, want text and initial image", prompts[0].Content)
			}
			if len(prompts[1].Content.Blocks) != 1 {
				t.Errorf("later prompt content = %#v, want text only", prompts[1].Content)
			}
		})
	}
}

func TestPrintModeTextPrintsTheFinalResponse(t *testing.T) {
	session, sessions := newPrintSession(t, createAssistantMessageT("hello from print"))
	var exitCode int
	output := captureStdout(t, func() {
		exitCode = RunPrintMode(session, sessions, PrintModeOptions{
			Mode:           CLIModeText,
			InitialMessage: "say hello",
		})
	})
	if exitCode != 0 {
		t.Fatalf("exit code = %d", exitCode)
	}
	if strings.TrimSpace(output) != "hello from print" {
		t.Fatalf("output = %q", output)
	}
	// The transcript survives: the loadout system message, the prompt, and the
	// response persisted. GetEntries excludes the header line.
	sessions.FlushWrites()
	entries := sessions.GetEntries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
}

func TestPrintModeTextReportsAnErroredResponse(t *testing.T) {
	errored := createAssistantMessageT("")
	errored.StopReason = ai.StopError
	message := "provider exploded"
	errored.ErrorMessage = &message
	session, sessions := newPrintSession(t, errored)
	var exitCode int
	output := captureStdout(t, func() {
		exitCode = RunPrintMode(session, sessions, PrintModeOptions{
			Mode:           CLIModeText,
			InitialMessage: "break",
		})
	})
	if exitCode != 1 {
		t.Fatalf("exit code = %d", exitCode)
	}
	if strings.Contains(output, "provider exploded") {
		t.Fatalf("error went to stdout: %q", output)
	}
}

func TestPrintModeWarnsAndReturnsFailureForTruncatedResponse(t *testing.T) {
	truncated := createAssistantMessageT("partial response")
	truncated.StopReason = ai.StopLength
	session, sessions := newPrintSession(t, truncated)
	var exitCode int
	var output string
	diagnostics := captureStdout(t, func() {
		stderr := os.Stderr
		os.Stderr = os.Stdout
		defer func() { os.Stderr = stderr }()
		output = captureStdout(t, func() {
			exitCode = RunPrintMode(session, sessions, PrintModeOptions{
				Mode: CLIModeText, InitialMessage: "finish this",
			})
		})
	})
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if strings.TrimSpace(output) != "partial response" {
		t.Fatalf("stdout = %q", output)
	}
	if warning := "Response was truncated: the model reached its output token limit."; !strings.Contains(diagnostics, warning) {
		t.Fatalf("stderr = %q, want warning %q", diagnostics, warning)
	}
}

func TestPrintModeJSONStreamsHeaderAndEvents(t *testing.T) {
	session, sessions := newPrintSession(t, createAssistantMessageT("reply"))
	var exitCode int
	output := captureStdout(t, func() {
		exitCode = RunPrintMode(session, sessions, PrintModeOptions{
			Mode:           CLIModeJSON,
			InitialMessage: "hi",
		})
	})
	if exitCode != 0 {
		t.Fatalf("exit code = %d", exitCode)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 { // header + at least agent_start..agent_end
		t.Fatalf("too few lines: %d", len(lines))
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header line: %v", err)
	}
	if header["type"] != "session" {
		t.Fatalf("header = %#v", header)
	}
	types := make([]string, 0, len(lines)-1)
	sawPartial := false
	for _, line := range lines[1:] {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("event line %q: %v", line, err)
		}
		typ, _ := event["type"].(string)
		types = append(types, typ)
		if inner, ok := event["assistantMessageEvent"].(map[string]any); ok {
			if _, has := inner["partial"]; has {
				sawPartial = true
			}
		}
	}
	if sawPartial {
		t.Fatal("message_update events must not carry cumulative partial snapshots")
	}
	if types[0] != "agent_start" {
		t.Fatalf("first event = %q", types[0])
	}
	// agent_settled is the session event emitted after the run (upstream
	// agent-session emits it after agent_end).
	if types[len(types)-1] != "agent_settled" {
		t.Fatalf("last event = %q", types[len(types)-1])
	}
	for _, wanted := range []string{"message_start", "message_end", "turn_end", "agent_end"} {
		found := false
		for _, typ := range types {
			if typ == wanted {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s event in %v", wanted, types)
		}
	}
}

func TestMarshalJSONSessionEventStripsPartialAndNamesToolCalls(t *testing.T) {
	assistant := createAssistantMessageT("")
	assistant.Content = ai.ContentList{ai.ToolCall{ID: "call_1", Name: "bash"}}
	event := &SessionEvent{
		Type: SessionMessageUpdate,
		Agent: &agent.AgentEvent{
			Type:                  agent.MessageUpdate,
			Message:               assistant,
			AssistantMessageEvent: &ai.AssistantMessageEvent{Type: ai.EventToolcallStart, ContentIndex: 0, Partial: assistant},
		},
	}
	encoded, err := MarshalJSONSessionEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	var shaped map[string]any
	if err := json.Unmarshal(encoded, &shaped); err != nil {
		t.Fatal(err)
	}
	inner, ok := shaped["assistantMessageEvent"].(map[string]any)
	if !ok {
		t.Fatalf("assistantMessageEvent = %#v", shaped)
	}
	if _, has := inner["partial"]; has {
		t.Fatal("partial must be stripped")
	}
	if inner["id"] != "call_1" || inner["toolName"] != "bash" {
		t.Fatalf("toolcall identity missing: %#v", inner)
	}
	if shaped["usage"] == nil {
		t.Fatal("usage must stay (constant size)")
	}
}

// The JSON event stream goes out as JSON.stringify, without HTML escaping.
func TestMarshalJSONSessionEventDoesNotHTMLEscape(t *testing.T) {
	assistant := createAssistantMessageT("a < b & c > d")
	event := &SessionEvent{
		Type:  SessionMessageEnd,
		Agent: &agent.AgentEvent{Type: agent.MessageEnd, Message: assistant},
	}
	encoded, err := MarshalJSONSessionEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `\u003c`) || strings.Contains(string(encoded), `\u0026`) || strings.Contains(string(encoded), `\u003e`) {
		t.Fatalf("HTML-escaped event: %s", encoded)
	}
	if !strings.Contains(string(encoded), "a < b & c > d") {
		t.Fatalf("event = %s", encoded)
	}
}
