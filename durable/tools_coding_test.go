package durable

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	env         ExecutionEnv
	output      strings.Builder
	diagnostics []ToolDiagnostic
}

func (a *fakeToolApi) Env() ExecutionEnv { return a.env }

func (a *fakeToolApi) Output(chunk []byte) { a.output.Write(chunk) }

func (a *fakeToolApi) Diagnostic(diagnostic ToolDiagnostic) {
	a.diagnostics = append(a.diagnostics, diagnostic)
}

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
	shell, err := NewOSShell(dir, "", nil)
	if err != nil {
		t.Skipf("no shell available: %v", err)
	}
	return &testToolEnv{OSFileSystem: fs, OSShell: shell}, dir
}

// Bounded-read expectations follow pi v1.1.0 packages/durable/src/tools/read.ts,
// env/line-scan.ts and tools/image.ts.
type readCountingEnv struct {
	*testToolEnv
	wholeFileReads int
}

func (e *readCountingEnv) ReadBinaryFile(path string, ctx context.Context) ([]byte, error) {
	e.wholeFileReads++
	return e.testToolEnv.ReadBinaryFile(path, ctx)
}

func TestReadToolDoesNotMisidentifyAnimatedPNGWithDistantChunk(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	png := pngBuffer(false)
	profile := make([]byte, 8+200_000+4)
	binary.BigEndian.PutUint32(profile[:4], 200_000)
	copy(profile[4:8], "iCCP")
	png = append(png, profile...)
	animation := []byte{0, 0, 0, 8, 'a', 'c', 'T', 'L', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	png = append(png, animation...)
	if err := env.WriteFile(filepath.Join(dir, "animated.png"), png, ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "animated.png"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != nil && *result.IsError {
		t.Fatalf("animated PNG was rejected as a supported image: %+v", result)
	}
}

func TestReadToolDoesNotLoadWholeFile(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "large.txt"), []byte("first\nsecond\n"), ctx); err != nil {
		t.Fatal(err)
	}
	counting := &readCountingEnv{testToolEnv: env}
	api := &fakeToolApi{env: counting}
	result, err := CreateReadTool().Execute(JsonObject{"path": "large.txt", "offset": float64(2), "limit": float64(1)}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "second" {
		t.Fatalf("content = %+v, want second line", result.Content)
	}
	if counting.wholeFileReads != 0 {
		t.Fatalf("whole-file reads = %d, want 0", counting.wholeFileReads)
	}
}

func TestReadToolOffsetsAndLimits(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "lines.txt"), []byte("one\ntwo\nthree\nfour"), ctx); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		args       JsonObject
		text       string
		diagnostic string
	}{
		{"offset and limit", JsonObject{"path": "lines.txt", "offset": float64(2), "limit": float64(2)}, "two\nthree", "1 more lines in file. Use offset=4 to continue."},
		{"zero limit", JsonObject{"path": "lines.txt", "offset": float64(2), "limit": float64(0)}, "", "3 more lines in file. Use offset=2 to continue."},
		{"negative limit", JsonObject{"path": "lines.txt", "offset": float64(2), "limit": float64(-1)}, "", "4 more lines in file. Use offset=1 to continue."},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := CreateReadTool().Execute(testCase.args, &fakeToolApi{env: env}, ctx)
			if err != nil {
				t.Fatal(err)
			}
			text := ""
			if len(result.Content) != 0 {
				text = result.Content[0].(ai.TextContent).Text
			}
			if text != testCase.text {
				t.Fatalf("text = %q, want %q", text, testCase.text)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != testCase.diagnostic {
				t.Fatalf("diagnostics = %+v, want %q", result.Diagnostics, testCase.diagnostic)
			}
		})
	}
}

func TestReadToolTruncatesAtDefaultLineLimit(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	lines := make([]string, DefaultMaxLines*5)
	for index := range lines {
		lines[index] = fmt.Sprintf("line-%d", index+1)
	}
	if err := env.WriteFile(filepath.Join(dir, "many.txt"), []byte(strings.Join(lines, "\n")), ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "many.txt"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details == nil {
		t.Fatal("missing truncation details")
	}
	details := result.Details.(ReadToolDetails)
	if details.Truncation == nil || !details.Truncation.Truncated || details.Truncation.TruncatedBy != "lines" ||
		details.Truncation.TotalLines != len(lines) || details.Truncation.OutputLines != DefaultMaxLines {
		t.Fatalf("truncation = %+v", details.Truncation)
	}
	if len(result.Content) != 1 || strings.Count(result.Content[0].(ai.TextContent).Text, "\n") != DefaultMaxLines-1 {
		t.Fatalf("content lines = %+v", result.Content)
	}
}

func TestReadToolReplacesInvalidUTF8(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "invalid.txt"), []byte{0xe2, 0x82, '(', 0xff, '\n'}, ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "invalid.txt"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "�(�\n" {
		t.Fatalf("content = %+v, want TextDecoder replacement characters", result.Content)
	}
}

func TestReadToolDoesNotTruncateAtExactLineLimitWithTrailingNewline(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	content := strings.Repeat("x\n", DefaultMaxLines)
	if err := env.WriteFile(filepath.Join(dir, "exact.txt"), []byte(content), ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "exact.txt"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("details = %+v, diagnostics = %+v", result.Details, result.Diagnostics)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != content {
		t.Fatalf("content does not preserve all %d terminated lines", DefaultMaxLines)
	}
}

func TestReadToolTruncatesLongUTF8LineAtCharacterBoundary(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	line := strings.Repeat("é", 40_000)
	if err := env.WriteFile(filepath.Join(dir, "long.txt"), []byte(line+"\nnext\n"), ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "long.txt"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != strings.Repeat("é", DefaultMaxBytes/2) {
		t.Fatal("long UTF-8 line was not cut at a character boundary")
	}
	if result.Details == nil {
		t.Fatal("missing truncation details")
	}
	details := result.Details.(ReadToolDetails)
	if details.Truncation == nil || !details.Truncation.FirstLineExceedsLimit ||
		details.Truncation.TotalBytes != len(line)+len("\nnext\n") || details.Truncation.OutputBytes != DefaultMaxBytes {
		t.Fatalf("truncation = %+v", details.Truncation)
	}
}

func TestReadToolStripsLeadingBOM(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	if err := env.WriteFile(filepath.Join(dir, "bom.txt"), []byte{0xef, 0xbb, 0xbf, 'o', 'n', 'e'}, ctx); err != nil {
		t.Fatal(err)
	}
	result, err := CreateReadTool().Execute(JsonObject{"path": "bom.txt"}, &fakeToolApi{env: env}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "one" {
		t.Fatalf("content = %+v, want one without BOM", result.Content)
	}
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
