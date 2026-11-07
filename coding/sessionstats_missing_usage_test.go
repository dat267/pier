package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.0 core/agent-session.ts getSessionStats reads
// `assistantMsg.usage` unconditionally and core/usage-totals.ts
// addUsageToTotals dereferences usage.input/output/cacheRead/cacheWrite and
// usage.cost.total, so a persisted assistant message without a complete usage
// record makes the whole call throw. Pier decodes usage into a value type, so
// the projection reads zeros and returns a result.
//
// Every message both sides write carries a complete usage record, so this only
// reaches the boundary through a hand-edited or foreign session file. The port
// keeps the tolerant read deliberately (a damaged transcript still reports its
// counts); this test pins that choice so it cannot change silently.
func TestSessionStatsToleratesAnIncompleteAssistantUsage(t *testing.T) {
	cases := []struct {
		name  string
		usage string
		input int64
	}{
		{"absent usage", ``, 0},
		{"null usage", `"usage":null,`, 0},
		{"partial usage", `"usage":{"input":5},`, 5},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "s.jsonl")
			body := `{"type":"session","version":3,"id":"s","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}` + "\n" +
				`{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"hi"}],"timestamp":1}}` + "\n" +
				`{"type":"message","id":"x","parentId":"m1","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"a"}],"api":"openai-completions","provider":"fake","model":"m",` + testCase.usage + `"stopReason":"stop","timestamp":2}}` + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			manager, err := OpenSession(path, dir, "")
			if err != nil {
				t.Fatal(err)
			}
			// The load path seeds the statistics scan off the caller's goroutine.
			waitForSeed(t, manager)
			stats := manager.SessionStats()
			if stats.AssistantMessages != 1 {
				t.Fatalf("assistant messages = %d, want 1", stats.AssistantMessages)
			}
			if stats.Tokens.Input != testCase.input {
				t.Fatalf("input tokens = %d, want %d", stats.Tokens.Input, testCase.input)
			}
		})
	}
}
