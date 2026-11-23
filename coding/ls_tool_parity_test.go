package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), core/tools/ls.ts with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/ls-tool/generate.mjs ./pi > coding/testdata/ls-tool/golden.json
func TestLsToolMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/ls-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string   `json:"name"`
		Path     *string  `json:"path"`
		Limit    *float64 `json:"limit"`
		Expected struct {
			Text    string          `json:"text"`
			Details json.RawMessage `json:"details"`
			Error   string          `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty ls-tool corpus")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tree", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "B.txt", "b.txt", "_x", "-y", ".hidden", "Z", "ä", "10", "9", "a b"} {
		if err := os.WriteFile(filepath.Join(dir, "tree", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "sub", "nested.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool := CreateLsTool(dir)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			params := map[string]any{}
			if fixture.Path != nil {
				params["path"] = *fixture.Path
			}
			if fixture.Limit != nil {
				params["limit"] = *fixture.Limit
			}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			result, execErr := tool.Execute(context.Background(), "call", encoded, nil)
			wantError := strings.ReplaceAll(fixture.Expected.Error, "<dir>", dir)
			checkParityToolResult(t, result, execErr, wantError, fixture.Expected.Text, fixture.Expected.Details)
		})
	}
}

// cancelOnSecondErrContext deterministically models cancellation after the
// tool's entry check, during its filesystem work.
type cancelOnSecondErrContext struct {
	context.Context
	checks int
	done   chan struct{}
}

func newCancelOnSecondErrContext() *cancelOnSecondErrContext {
	return &cancelOnSecondErrContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *cancelOnSecondErrContext) Done() <-chan struct{} { return c.done }

func (c *cancelOnSecondErrContext) Err() error {
	c.checks++
	if c.checks >= 2 {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
		return context.Canceled
	}
	return nil
}

// pi v1.4.0 core/tools/ls.ts rejects with "Operation aborted" if its
// AbortSignal fires during the awaited filesystem operations.
func TestLsToolAbortsWhenCanceledDuringFilesystemWork(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "entry.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := newCancelOnSecondErrContext()
	_, err := CreateLsTool(dir).Execute(ctx, "call", json.RawMessage(`{}`), nil)
	if err == nil || err.Error() != "Operation aborted" {
		t.Fatalf("ls error = %v; want Operation aborted", err)
	}
}
