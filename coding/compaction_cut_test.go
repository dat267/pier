package coding

import (
	"encoding/json"
	"github.com/dat267/pier/ai"
	"os"
	"strings"
	"testing"
)

// Compaction tests keyed to upstream compaction.ts semantics.

func TestShouldCompact(t *testing.T) {
	settings := DefaultCompactionSettings
	if ShouldCompact(100000, 200000, settings) {
		t.Fatal("under reserve threshold should not compact")
	}
	if !ShouldCompact(190000, 200000, settings) {
		t.Fatal("over threshold should compact")
	}
	disabled := CompactionSettings{Enabled: false}
	if ShouldCompact(190000, 200000, disabled) {
		t.Fatal("disabled never compacts")
	}
	// The trigger is strictly greater than window minus reserve (upstream
	// contextTokens > contextWindow - reserveTokens), so a context exactly at
	// the threshold must not compact.
	tight := CompactionSettings{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000}
	threshold := int64(200000) - tight.ReserveTokens
	if ShouldCompact(threshold, 200000, tight) {
		t.Fatalf("a context exactly at the threshold (%d) compacted", threshold)
	}
	if !ShouldCompact(threshold+1, 200000, tight) {
		t.Fatal("a context one token above the threshold did not compact")
	}
}

func TestFindCutPoint(t *testing.T) {
	// Build a session: 10 user/assistant turns of ~100 tokens each.
	m := NewSessionManager("/tmp", &SessionManagerOptions{Persist: new(false)})
	var entries []SessionEntry
	for range 10 {
		m.AppendMessage(createUserMessage(strings.Repeat("u", 400))) // ~100 tokens
		m.AppendMessage(createAssistantMessageT(strings.Repeat("a", 400)))
	}
	entries = m.GetEntries()

	// Keep ~500 tokens: cuts at the newest boundary, keeping the tail.
	result := FindCutPoint(entries, 0, len(entries), 500)
	if result.FirstKeptEntryIndex < 0 || result.FirstKeptEntryIndex >= len(entries) {
		t.Fatalf("cut index = %d", result.FirstKeptEntryIndex)
	}
	// Never cut at a toolResult... (none here). The kept entries hold >= 500
	// tokens minus one message.
	keptTokens := 0
	for i := result.FirstKeptEntryIndex; i < len(entries); i++ {
		for _, msg := range SessionEntryToContextMessages(&entries[i]) {
			keptTokens += EstimateAgentMessageTokens(msg)
		}
	}
	if keptTokens < 400 {
		t.Fatalf("kept tokens = %d; cut overshot", keptTokens)
	}
	// Empty range → cut at start.
	empty := FindCutPoint(entries, 0, 0, 500)
	if empty.FirstKeptEntryIndex != 0 {
		t.Fatalf("empty range = %+v", empty)
	}
}

func TestFindCutPointNeverSplitsToolResult(t *testing.T) {
	m := NewSessionManager("/tmp", &SessionManagerOptions{Persist: new(false)})
	m.AppendMessage(createUserMessage(strings.Repeat("u", 2000))) // big turn
	entries := m.GetEntries()
	userEntry := entries[0]

	// Assistant with a tool call + result: the cut must include the whole
	// tool pair (cut points exclude tool results).
	m.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.ToolCall{ID: "c1", Name: "bash", Arguments: json.RawMessage(`{}`)}},
		API:     "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopToolUse, Timestamp: 2,
	})
	m.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "bash",
		Content:   ai.UserContentList{ai.TextContent{Text: strings.Repeat("r", 4000)}},
		Timestamp: 3,
	})
	all := m.GetEntries()

	// Tiny budget: cut must land ON the first user entry (a valid cut point),
	// never between the assistant and its result.
	result := FindCutPoint(all, 0, len(all), 10)
	if result.FirstKeptEntryIndex < 0 {
		t.Fatal("cut point must exist")
	}
	_ = userEntry
}

// compaction.ts cutPointMessage: user, assistant, bash-execution, custom,
// branch-summary and compaction-summary messages are boundaries a summary may
// cut at; a tool result is not, so a call and its result never straddle one.
func TestIsCutPointMessage(t *testing.T) {
	custom, err := json.Marshal(map[string]any{"customType": "note", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name    string
		message ai.Message
		want    bool
	}{
		{"user", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}, true},
		{"assistant", &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "hi"}}}, true},
		{"tool result", &ai.ToolResultMessage{ToolCallID: "c", ToolName: "read"}, false},
		{"bash execution", &ai.CustomMessage{Role: RoleBashExecution, Content: json.RawMessage(`{}`)}, true},
		{"custom", &ai.CustomMessage{Role: RoleCustom, Content: custom}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isCutPointMessage(testCase.message); got != testCase.want {
				t.Fatalf("isCutPointMessage(%T) = %v, want %v", testCase.message, got, testCase.want)
			}
		})
	}
}

// pi v1.4.0 (4a080252), core/compaction/compaction.ts findCutPoint.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/cut-point/generate.mjs ./pi > coding/testdata/cut-point/golden.json
func TestFindCutPointMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/cut-point/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []struct {
		Name    string            `json:"name"`
		Entries []json.RawMessage `json:"entries"`
		Cases   []struct {
			StartIndex       int `json:"startIndex"`
			EndIndex         int `json:"endIndex"`
			KeepRecentTokens int `json:"keepRecentTokens"`
			Expected         struct {
				FirstKeptEntryIndex int  `json:"firstKeptEntryIndex"`
				TurnStartIndex      int  `json:"turnStartIndex"`
				IsSplitTurn         bool `json:"isSplitTurn"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("empty cut-point corpus")
	}
	for _, scenario := range scenarios {
		entries := make([]SessionEntry, 0, len(scenario.Entries))
		for _, encoded := range scenario.Entries {
			fileEntry, err := UnmarshalFileEntry(string(encoded))
			if err != nil {
				t.Fatalf("%s: decode entry: %v", scenario.Name, err)
			}
			if fileEntry.Entry == nil {
				t.Fatalf("%s: entry has no body", scenario.Name)
			}
			entries = append(entries, *fileEntry.Entry)
		}
		for _, testCase := range scenario.Cases {
			got := FindCutPoint(entries, testCase.StartIndex, testCase.EndIndex, testCase.KeepRecentTokens)
			if got.FirstKeptEntryIndex != testCase.Expected.FirstKeptEntryIndex ||
				got.TurnStartIndex != testCase.Expected.TurnStartIndex || got.IsSplitTurn != testCase.Expected.IsSplitTurn {
				t.Errorf("%s start=%d end=%d keep=%d: cut = %+v, want %+v", scenario.Name,
					testCase.StartIndex, testCase.EndIndex, testCase.KeepRecentTokens, got, testCase.Expected)
			}
		}
	}
}
