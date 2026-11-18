package coding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"weak"
)

// seedSession creates a persisted session with a full turn (the file flushes
// only once an assistant message exists, upstream flush-on-first-assistant).
func seedSession(t *testing.T, cwd string) *SessionManager {
	t.Helper()
	sm := NewSessionManager(cwd, nil)
	sm.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	sm.AppendMessage(&ai.AssistantMessage{
		API:        ai.APIAnthropicMessages,
		Provider:   "anthropic",
		Model:      "m",
		Content:    ai.ContentList{ai.TextContent{Text: "hi"}},
		StopReason: ai.StopStop,
	})
	return sm
}

// TestListAllSessionsIncludesAllProjects asserts the global scan returns
// sessions from every project dir regardless of their header cwd (upstream
// listAll -> listSessionsFromDir, which never cwd-filters).
func TestListAllSessionsIncludesAllProjects(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	cwdA := t.TempDir()
	cwdB := t.TempDir()
	a := seedSession(t, cwdA)
	b := seedSession(t, cwdB)

	all := ListAllSessions("")
	ids := map[string]bool{}
	for _, info := range all {
		ids[info.ID] = true
	}
	if !ids[a.GetSessionID()] {
		t.Fatalf("ListAllSessions missing session %s from %s", a.GetSessionID(), cwdA)
	}
	if !ids[b.GetSessionID()] {
		t.Fatalf("ListAllSessions missing session %s from %s", b.GetSessionID(), cwdB)
	}
}

// TestListAllSessionsExplicitDir asserts the explicit-directory overload lists
// that directory without a cwd filter (upstream listAll(customSessionDir)).
func TestListAllSessionsExplicitDir(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	sessionDir := t.TempDir()
	otherCwd := t.TempDir()
	sm := NewSessionManager(otherCwd, &SessionManagerOptions{SessionDir: sessionDir})
	sm.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	sm.AppendMessage(&ai.AssistantMessage{
		API:        ai.APIAnthropicMessages,
		Provider:   "anthropic",
		Model:      "m",
		Content:    ai.ContentList{ai.TextContent{Text: "hi"}},
		StopReason: ai.StopStop,
	})

	all := ListAllSessions(sessionDir)
	found := false
	for _, info := range all {
		if info.ID == sm.GetSessionID() {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListAllSessions(%q) missing session %s", sessionDir, sm.GetSessionID())
	}
}

// writeListingFixture writes count session files of about size bytes each, so a
// listing has real work to do per file.
func writeListingFixture(t *testing.T, count int, size int) string {
	t.Helper()
	dir := t.TempDir()
	body := make([]byte, size)
	for index := range body {
		body[index] = 'x'
	}
	text := string(body)
	for index := range count {
		name := fmt.Sprintf("%02d.jsonl", index)
		content := `{"type":"session","version":3,"id":"s` + name + `","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}` + "\n" +
			`{"type":"message","id":"m` + name + `","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Upstream listSessionsFromDir publishes a sorted partial list as files finish
// (first file, every CURRENT_SESSION_LIST_PUBLISH_INTERVAL files, and the last),
// so the picker shows rows while the rest still load.
func TestListSessionsWithProgressReportsPartials(t *testing.T) {
	dir := writeListingFixture(t, 12, 1024)
	var updates []struct {
		loaded int
		total  int
		rows   int
	}
	sessions, err := ListSessionsWithProgress(context.Background(), dir, dir, func(loaded int, total int, partial []SessionInfo) {
		updates = append(updates, struct {
			loaded int
			total  int
			rows   int
		}{loaded: loaded, total: total, rows: len(partial)})
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 12 {
		t.Fatalf("sessions = %d, want 12", len(sessions))
	}
	if len(updates) < 3 {
		t.Fatalf("progress updates = %d, want at least 3 (first, every 10, last): %v", len(updates), updates)
	}
	if updates[0].loaded != 1 || updates[0].rows != 1 {
		t.Fatalf("first update = %+v, want one row of one file", updates[0])
	}
	for index, update := range updates {
		if update.total != 12 {
			t.Fatalf("update %d total = %d, want 12", index, update.total)
		}
		if index > 0 && update.loaded <= updates[index-1].loaded {
			t.Fatalf("loaded did not grow: %v", updates)
		}
		if update.rows != update.loaded {
			t.Fatalf("update %d carried %d rows for %d files", index, update.rows, update.loaded)
		}
	}
	if last := updates[len(updates)-1]; last.loaded != 12 || last.rows != 12 {
		t.Fatalf("last update = %+v, want all 12", last)
	}
}

// A cancelled load stops instead of reading every file: the picker cancels when
// the user switches scope, and the port used to keep scanning.
func TestListSessionsWithProgressStopsOnCancel(t *testing.T) {
	// Enough files that a long tail remains after the first publish, which is
	// what the cancel can actually skip: the progress interval is 10 files and
	// the listing runs 10 workers, so a 12-file fixture leaves nothing to prove.
	dir := writeListingFixture(t, 60, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	_, err := ListSessionsWithProgress(ctx, dir, dir, func(loaded int, total int, partial []SessionInfo) {
		seen = loaded
		if seen >= 1 {
			cancel()
		}
	})
	if err == nil {
		t.Fatalf("a cancelled listing returned no error (read %d of 12 files)", seen)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if seen >= 60 {
		t.Fatalf("the listing read every file after cancellation: %d", seen)
	}
}

// writeListingFixtureForBench is writeListingFixture for benchmarks.
func writeListingFixtureForBench(b *testing.B, count int, size int) string {
	b.Helper()
	dir := b.TempDir()
	body := make([]byte, size)
	for index := range body {
		body[index] = 'x'
	}
	text := string(body)
	for index := range count {
		name := fmt.Sprintf("%02d.jsonl", index)
		content := `{"type":"session","version":3,"id":"s` + name + `","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}` + "\n" +
			`{"type":"message","id":"m` + name + `","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

// The listing scanner must agree with the typed decoder for every message it
// claims to understand; anything it rejects falls back to that decoder.
func TestScanListingMessageMatchesTypedDecode(t *testing.T) {
	marshal := func(message ai.Message) string {
		t.Helper()
		raw, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	assistantWithToolCall := marshal(&ai.AssistantMessage{
		Content: ai.ContentList{
			ai.TextContent{Text: "answer"},
			ai.ThinkingContent{Thinking: "long reasoning"},
			ai.ToolCall{ID: "call_1", Name: "bash", Arguments: []byte(`{"command":"ls"}`)},
		},
		Timestamp: 42,
	})
	userWithImage := marshal(&ai.UserMessage{
		Content: ai.StringOrBlocks{Blocks: ai.ContentList{
			ai.TextContent{Text: "look at this"},
			ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
		}},
		Timestamp: 7,
	})
	systemMessage := marshal(&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "sys"}, Timestamp: 3})

	cases := []struct {
		name string
		raw  string
	}{
		{"user string", `{"role":"user","content":"hello","timestamp":5}`},
		{"user string content first", `{"content":"hello","role":"user","timestamp":5}`},
		{"user empty string", `{"role":"user","content":"","timestamp":5}`},
		{"user null content", `{"role":"user","content":null,"timestamp":5}`},
		{"user blocks", userWithImage},
		{"user escaped content", `{"role":"user","content":"line\n\"quoted\" \u00e9","timestamp":5}`},
		{"user missing timestamp", `{"role":"user","content":"hello"}`},
		{"user null timestamp", `{"role":"user","content":"hello","timestamp":null}`},
		{"user negative timestamp", `{"role":"user","content":"hello","timestamp":-5}`},
		{"assistant blocks", assistantWithToolCall},
		{"tool result", `{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"output"}],"timestamp":9}`},
		{"tool result content first", `{"content":[{"type":"text","text":"output"}],"role":"toolResult","toolCallId":"c1","toolName":"bash","timestamp":9}`},
		{"system", systemMessage},
		{"custom role", `{"role":"bashExecution","content":[{"type":"text","text":"x"}],"timestamp":4}`},
		{"missing role", `{"content":"x","timestamp":4}`},
		{"empty object", `{}`},
		{"unknown top-level member", `{"role":"user","content":"hello","timestamp":5,"extra":{"a":1}}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scanned := scanListingMessage([]byte(testCase.raw))
			message, err := ai.UnmarshalMessage([]byte(testCase.raw))
			if err != nil {
				t.Fatalf("reference decode failed: %v", err)
			}
			role := string(ai.RoleOf(message))
			text := ""
			if role == "user" || role == "assistant" {
				text = extractTextContent(message)
			}
			if !scanned.ok {
				t.Fatalf("scanner rejected a decodable message")
			}
			if scanned.role != role || scanned.timestamp != listingMessageTimestamp(message) || scanned.text != text {
				t.Fatalf("scanner = %+v, reference role=%q timestamp=%d text=%q",
					scanned, role, listingMessageTimestamp(message), text)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(testCase.raw), &fields); err != nil {
				t.Fatal(err)
			}
			_, wantContent := fields["content"]
			if scanned.hasContent != wantContent {
				t.Fatalf("scanner hasContent = %v, want %v", scanned.hasContent, wantContent)
			}
			var number float64
			wantTimestamp := false
			if raw, ok := fields["timestamp"]; ok && json.Unmarshal(raw, &number) == nil {
				wantTimestamp = true
			}
			if scanned.hasTimestamp != wantTimestamp {
				t.Fatalf("scanner hasTimestamp = %v, want %v", scanned.hasTimestamp, wantTimestamp)
			}
		})
	}

	// Shapes the scanner cannot take at face value fall back instead of
	// guessing: the caller then full-decodes.
	rejected := []struct {
		name string
		raw  string
	}{
		{"unknown block type", `{"role":"assistant","content":[{"type":"weird"}],"timestamp":1}`},
		{"non-string text", `{"role":"assistant","content":[{"type":"text","text":123}],"timestamp":1}`},
		{"non-object message", `"nope"`},
		{"non-string role", `{"role":5,"content":"x"}`},
		{"duplicate role", `{"role":"user","role":"assistant","content":"x"}`},
		{"trailing data", `{"role":"user","content":"x"} extra`},
	}
	for _, testCase := range rejected {
		t.Run(testCase.name, func(t *testing.T) {
			if scanned := scanListingMessage([]byte(testCase.raw)); scanned.ok {
				t.Fatalf("scanner accepted %q: %+v", testCase.raw, scanned)
			}
		})
	}
}

// Listing metadata must not allocate proportional to block payloads it does
// not read: tool-call arguments, thinking and tool-result bodies dominate real
// sessions. The typed decode allocated the payload strings; the scanner skips
// them.
func TestBuildSessionInfoSkipsUnusedMessagePayloads(t *testing.T) {
	thinking := strings.Repeat("reasoning payload ", 512) // ~9 KiB
	var buf strings.Builder
	buf.WriteString(`{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}` + "\n")
	for i := range 400 {
		// Alternate key order: both exist in stored sessions.
		if i%2 == 0 {
			fmt.Fprintf(&buf, `{"type":"message","id":"m%04d","parentId":null,"timestamp":"t","message":{"role":"assistant","content":[{"type":"text","text":"hello %d"},{"type":"thinking","thinking":%q}],"timestamp":%d}}`+"\n", i, i, thinking, i+1)
		} else {
			fmt.Fprintf(&buf, `{"type":"message","id":"m%04d","parentId":null,"timestamp":"t","message":{"content":[{"type":"text","text":"hello %d"},{"type":"thinking","thinking":%q}],"role":"assistant","timestamp":%d}}`+"\n", i, i, thinking, i+1)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	allocated := allocatedBytesPerRun(t, 1, func() {
		built := buildSessionInfo(path)
		if built == nil || built.MessageCount != 400 || strings.Count(built.AllMessagesText, "hello") != 400 {
			t.Fatalf("buildSessionInfo = %+v", built)
		}
	})
	if allocated > int64(info.Size())*2 {
		t.Fatalf("buildSessionInfo allocated %d bytes for a %d-byte file of unread payloads", allocated, info.Size())
	}
}

// listingMessageTimestamp is the numeric timestamp a decoded message carries
// (the scanner's counterpart before presence handling).
func listingMessageTimestamp(message ai.Message) int64 {
	switch m := message.(type) {
	case *ai.SystemMessage:
		return m.Timestamp
	case *ai.UserMessage:
		return m.Timestamp
	case *ai.AssistantMessage:
		return m.Timestamp
	case *ai.ToolResultMessage:
		return m.Timestamp
	case *ai.CustomMessage:
		return m.Timestamp
	}
	return 0
}

// pi v1.4.0 (4a080252), core/agent-session.ts subscribe removes listeners
// with Array.splice. A detached UI's captured state must not survive solely in
// the Go listener slice's unused backing tail.
func TestSessionUnsubscribeReleasesListenerCapture(t *testing.T) {
	dir := t.TempDir()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, AgentDir: dir, Sessions: InMemorySession(dir, nil, nil), StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Dispose)
	payload := subscribeThenRemoveSessionCapture(session)
	runtime.GC()
	if payload.Value() != nil {
		t.Fatal("unsubscribed session listener retains captured payload")
	}
	runtime.KeepAlive(session)
}

func subscribeThenRemoveSessionCapture(session *AgentSession) weak.Pointer[[1024]byte] {
	payload := new([1024]byte)
	pointer := weak.Make(payload)
	remove := session.Subscribe(func(*SessionEvent) { runtime.KeepAlive(payload) })
	remove()
	return pointer
}
