package durable

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of the durable read/write tools.

type testToolEnv struct {
	*OSFileSystem
	*OSShell
}

type fakeToolApi struct {
	ToolExecutionApi
	env ExecutionEnv
}

func (a *fakeToolApi) Env() ExecutionEnv { return a.env }

func (e *testToolEnv) Cwd() string                       { return e.OSFileSystem.Cwd() }
func (e *testToolEnv) SetCwd(path string)                { e.OSFileSystem.SetCwd(path) }
func (e *testToolEnv) Cleanup(ctx context.Context) error { return e.OSFileSystem.Cleanup(ctx) }

func newToolTestEnv(t *testing.T) (*testToolEnv, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	fs, err := NewOSFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &testToolEnv{OSFileSystem: fs}, dir
}

func TestReadTool(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), ctx); err != nil {
		t.Fatal(err)
	}
	api := &fakeToolApi{env: env}
	result, err := CreateReadTool().Execute(JsonObject{"path": "a.txt"}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v", result.Content)
	}
	text := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "one") || !strings.Contains(text, "three") {
		t.Fatalf("text = %q", text)
	}
	// An offset beyond the file is an error.
	if _, err := CreateReadTool().Execute(JsonObject{"path": "a.txt", "offset": float64(99)}, api, ctx); err == nil {
		t.Fatal("an out-of-range offset must fail")
	}
	// An image is rejected with the unsupported_image code.
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}
	if err := env.WriteFile(filepath.Join(dir, "b.png"), png, ctx); err != nil {
		t.Fatal(err)
	}
	result, err = CreateReadTool().Execute(JsonObject{"path": "b.png"}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError == nil || !*result.IsError || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code == nil || *result.Diagnostics[0].Code != "unsupported_image" {
		t.Fatalf("result = %+v", result)
	}
}

func TestWriteTool(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	api := &fakeToolApi{env: env}
	result, err := CreateWriteTool().Execute(JsonObject{"path": "out.txt", "content": "hello"}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "Successfully wrote to out.txt" {
		t.Fatalf("result = %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("content = %q, %v", content, err)
	}
}

var _ = json.Marshal
