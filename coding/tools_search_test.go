package coding

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// find/grep tests against the real fd/rg binaries (the tools' default
// backends). Skipped when the binary is unavailable.

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
}

// pi v1.5.2 (b4afdddf) find.ts floors the limit and asks for one result past it
// (`Math.max(1, Math.floor(limit ?? DEFAULT_LIMIT))`), so a fractional limit no
// longer reaches fd as an invalid --max-results value.
func TestFindToolFloorsFractionalLimitForFD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX fd test stub")
	}
	argsFile := filepath.Join(t.TempDir(), "args")
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "fd")
	script := "#!/bin/sh\nfor arg do\n  if [ \"$arg\" = \"1.5\" ]; then echo 'invalid max-results: 1.5' >&2; exit 2; fi\ndone\nprintf '%s\\n' \"$*\" > " + argsFile + "\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool := CreateFindTool(t.TempDir())
	if _, err := tool.Execute("c", json.RawMessage(`{"pattern":"*.txt","limit":1.5}`), context.Background(), nil); err != nil {
		t.Fatalf("fractional find limit error = %v", err)
	}
	recorded, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recorded), "--max-results 2 ") {
		t.Fatalf("fd args = %q; want the floored limit plus one", recorded)
	}
}

// D244: upstream find.ts trims each line before relativizing it, which breaks a
// file whose name legitimately begins or ends with a space. The port keeps the
// whitespace so the returned path stays readable.
func TestFindToolPreservesWhitespaceInFileNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow leading/trailing spaces in file names")
	}
	requireBinary(t, "fd")
	dir := t.TempDir()
	name := " spaced "
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateFindTool(dir), `{"pattern":"*"}`)
	got := result.Content[0].(ai.TextContent).Text
	if got != name {
		t.Fatalf("find path = %q; want %q", got, name)
	}
}

// find.ts consumes fd output through readline's UTF-8 string decoding.
func TestFindToolReplacesMalformedUTF8Paths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permits invalid UTF-8 file names")
	}
	requireBinary(t, "fd")
	dir := t.TempDir()
	name := string([]byte{0xff, 'x'})
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateFindTool(dir), `{"pattern":"*"}`)
	got := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(got, "\uFFFDx") || strings.Contains(got, name) {
		t.Fatalf("find output path = %q; want replacement-decoded name", got)
	}
}

func TestFindToolGlob(t *testing.T) {
	requireBinary(t, "fd")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "a.ts"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "deep", "b.spec.ts"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("x"), 0o644)

	tool := CreateFindTool(dir)
	result := execTool(t, tool, `{"pattern":"*.ts"}`)
	text := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "src/a.ts") || strings.Contains(text, "readme.md") {
		t.Fatalf("find = %q", text)
	}

	// Path-containing glob.
	result = execTool(t, tool, `{"pattern":"src/**/*.spec.ts"}`)
	text = result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "src/deep/b.spec.ts") {
		t.Fatalf("find full-path = %q", text)
	}

	// No match.
	result = execTool(t, tool, `{"pattern":"*.nope"}`)
	text = result.Content[0].(ai.TextContent).Text
	if text != "No files found matching pattern" {
		t.Fatalf("text = %q", text)
	}
}

func TestFindToolRespectsGitignore(t *testing.T) {
	requireBinary(t, "fd")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "ignored"), 0o755)
	os.WriteFile(filepath.Join(dir, "ignored", "x.ts"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "keep.ts"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755) // make it a git repo

	tool := CreateFindTool(dir)
	result := execTool(t, tool, `{"pattern":"*.ts"}`)
	text := result.Content[0].(ai.TextContent).Text
	if strings.Contains(text, "ignored") {
		t.Fatalf("gitignored file surfaced: %q", text)
	}
	if !strings.Contains(text, "keep.ts") {
		t.Fatalf("text = %q", text)
	}
}

func TestFindRelativizePosix(t *testing.T) {
	if got := RelativizeFindResultPath("/base/sub/f.txt", "/base"); got != "sub/f.txt" {
		t.Fatalf("relativize = %q", got)
	}
	if got := RelativizeFindResultPath("already/relative.txt", "/base"); got != "already/relative.txt" {
		t.Fatalf("relative = %q", got)
	}
}

// pi v1.5.2 grep.ts keeps reading after the limit match so the last match's
// trailing context can arrive, and finishes once another match proves the limit
// cut results off.
func TestGrepToolStopsRipgrepAtMatchLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX rg test stub")
	}
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "rg")
	first := `{"type":"match","data":{"path":{"text":"` + filepath.Join(stubDir, "match.txt") + `"},"line_number":1,"lines":{"text":"needle\\n"}}}`
	second := `{"type":"match","data":{"path":{"text":"` + filepath.Join(stubDir, "match.txt") + `"},"line_number":2,"lines":{"text":"needle again\\n"}}}`
	script := "#!/bin/sh\nprintf '%s\\n' '" + first + "'\nsleep 0.2\nprintf '%s\\n' '" + second + "'\nexec sleep 30\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stubDir, "match.txt"), []byte("needle\nneedle again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := CreateGrepTool(stubDir).Execute("c", json.RawMessage(`{"pattern":"needle","limit":1}`), ctx, nil)
	if err != nil {
		t.Fatalf("grep did not finish after reaching limit: %v", err)
	}
	text := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "match.txt:1: needle") {
		t.Fatalf("grep output = %q", text)
	}
	if strings.Contains(text, "needle again") || !strings.Contains(text, "1 matches limit reached") {
		t.Fatalf("limit output = %q", text)
	}
}

func TestGrepToolMatches(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha one\nbeta two\nalpha three\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing here\n"), 0o644)

	tool := CreateGrepTool(dir)
	result := execTool(t, tool, `{"pattern":"alpha"}`)
	text := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "a.txt:1: alpha one") || !strings.Contains(text, "a.txt:3: alpha three") {
		t.Fatalf("grep = %q", text)
	}
	if strings.Contains(text, "b.txt") {
		t.Fatalf("non-matching file in output: %q", text)
	}

	// pi v1.5.2 floors the limit and never goes below 1.
	result = execTool(t, tool, `{"pattern":"alpha","limit":1.5}`)
	text = result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "a.txt:1: alpha one") || strings.Contains(text, "alpha three") ||
		!strings.Contains(text, "1 matches limit reached. Use limit=2 for more") {
		t.Fatalf("fractional grep limit = %q", text)
	}

	// Literal (non-regex) search: pattern with regex metachars.
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("a.b\naxb\n"), 0o644)
	result = execTool(t, tool, `{"pattern":"a.b","literal":true}`)
	text = result.Content[0].(ai.TextContent).Text
	if strings.Contains(text, "axb") || !strings.Contains(text, "a.b") {
		t.Fatalf("literal grep = %q", text)
	}

	// ignoreCase.
	result = execTool(t, tool, `{"pattern":"ALPHA","ignoreCase":true}`)
	text = result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "a.txt:1:") {
		t.Fatalf("ignoreCase grep = %q", text)
	}

	// glob filter.
	result = execTool(t, tool, `{"pattern":"alpha","glob":"b.txt"}`)
	text = result.Content[0].(ai.TextContent).Text
	if text != "No matches found" {
		t.Fatalf("glob grep = %q", text)
	}

	// Context lines.
	result = execTool(t, tool, `{"pattern":"beta two","context":1}`)
	text = result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "a.txt-1- alpha one") || !strings.Contains(text, "a.txt:2: beta two") || !strings.Contains(text, "a.txt-3- alpha three") {
		t.Fatalf("context grep = %q", text)
	}

	// pi v1.5.2 floors the context, so a fraction below one means no context.
	result = execTool(t, tool, `{"pattern":"beta two","context":0.5}`)
	text = result.Content[0].(ai.TextContent).Text
	if text != "a.txt:2: beta two" {
		t.Fatalf("fractional context grep = %q", text)
	}

	// Long-line truncation.
	os.WriteFile(filepath.Join(dir, "long.txt"), []byte(strings.Repeat("x", 600)+"NEEDLE\n"), 0o644)
	result = execTool(t, tool, `{"pattern":"NEEDLE"}`)
	text = result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "... [truncated]") {
		t.Fatalf("long line = %q", text)
	}
	var details GrepToolDetails
	json.Unmarshal(result.Details, &details)
	if details.LinesTruncated == nil || !*details.LinesTruncated {
		t.Fatalf("details = %s", result.Details)
	}
}

// grep.ts's default context-file reader uses fs.readFile(..., "utf-8").
func TestGrepToolContextReplacesMalformedUTF8(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "invalid.txt"), []byte{0xff, '\n', 'n', 'e', 'e', 'd', 'l', 'e', '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateGrepTool(dir), `{"pattern":"needle","context":1}`)
	got := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(got, "invalid.txt-1- \uFFFD") {
		t.Fatalf("grep context = %q; want replacement rune for invalid byte", got)
	}
}

// grep.ts falls back to the UTF-8 context reader when rg supplies `lines.bytes`.
func TestGrepToolReplacesMalformedUTF8MatchLine(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "invalid.txt"), []byte{0xff, 'n', 'e', 'e', 'd', 'l', 'e', '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateGrepTool(dir), `{"pattern":"needle"}`)
	got := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(got, "invalid.txt:1: \uFFFDneedle") {
		t.Fatalf("grep invalid UTF-8 line = %q; want replacement-decoded match", got)
	}
}

func TestGrepToolErrors(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	tool := CreateGrepTool(dir)

	// Missing path.
	_, err := tool.Execute("c", json.RawMessage(`{"pattern":"x","path":"nope/"}`), context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "Path not found") {
		t.Fatalf("err = %v", err)
	}

	// Bad regex is an rg error surfaced in-band.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	_, err = tool.Execute("c", json.RawMessage(`{"pattern":"[unclosed"}`), context.Background(), nil)
	if err == nil {
		t.Fatal("bad regex should error")
	}
}

// Upstream find.ts reports spawn failures as "Failed to run fd: ..." through
// child.on("error"); the port must not dereference a process state the failed
// spawn never produced.
func TestFindToolReportsSpawnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX fd test stub")
	}
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "fd")
	if err := os.WriteFile(stub, []byte("not an executable\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool := CreateFindTool(t.TempDir())
	_, err := tool.Execute("c", json.RawMessage(`{"pattern":"*.txt"}`), context.Background(), nil)
	if err == nil || !strings.HasPrefix(err.Error(), "Failed to run fd: ") {
		t.Fatalf("spawn failure error = %v; want Failed to run fd: ...", err)
	}
}

// Upstream find.ts rewrites '/' to a [/\\] class on win32 so fd --full-path
// patterns match native separators in the absolute candidate path.
func TestFindFullPathVariable(t *testing.T) {
	cases := []struct {
		name, pattern, goos, want string
		fullPath                  bool
	}{
		{"basename", "*.ts", "linux", "*.ts", false},
		{"basename windows", "*.ts", "windows", "*.ts", false},
		{"prefixed", "src/**/*.ts", "linux", "**/src/**/*.ts", true},
		{"prefixed windows", "src/**/*.ts", "windows", `**[/\\]src[/\\]**[/\\]*.ts`, true},
		{"leading slash windows", "/src/*.ts", "windows", `[/\\]src[/\\]*.ts`, true},
		{"globstar prefix windows", "**/x.ts", "windows", `**[/\\]x.ts`, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, fullPath := findFullPathVariable(test.pattern, test.goos)
			if got != test.want || fullPath != test.fullPath {
				t.Fatalf("findFullPathVariable(%q, %q) = (%q, %v); want (%q, %v)", test.pattern, test.goos, got, fullPath, test.want, test.fullPath)
			}
		})
	}
}

// The grep tool's contract must not depend on ambient ripgrep configuration.
// Upstream v1.4.0 8c4e87389 (issue #11203): the tool now spawns rg with
// --no-config, so RIPGREP_CONFIG_PATH (a config containing --fixed-strings, say)
// cannot turn a regex search into a literal one.
func TestGrepToolIgnoresRipgrepConfig(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	configFile := filepath.Join(t.TempDir(), "rg-config")
	if err := os.WriteFile(configFile, []byte("--fixed-strings\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RIPGREP_CONFIG_PATH", configFile)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aXb\na.b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := CreateGrepTool(dir)
	result := execTool(t, tool, `{"pattern":"a.b"}`)
	text := result.Content[0].(ai.TextContent).Text
	// A regex search matches both lines; a literal search (what the config would
	// force) matches only the second.
	if !strings.Contains(text, "aXb") {
		t.Fatalf("grep honored RIPGREP_CONFIG_PATH --fixed-strings, so the regex became literal: %q", text)
	}
	if !strings.Contains(text, "a.b") {
		t.Fatalf("regex search lost its match: %q", text)
	}
}

// pi v1.5.0 grep.ts normalizes only CRLF when it reads a file for context, so a
// lone CR does not start a new line and the line numbers that follow it stay
// those ripgrep reported (the output still strips the CR itself).
func TestGrepToolDoesNotSplitOnLoneCarriageReturn(t *testing.T) {
	requireBinary(t, "rg")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mac.txt"), []byte("alpha\rbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := CreateGrepTool(dir)
	result := execTool(t, tool, `{"pattern":"gamma"}`)
	text := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "mac.txt:2: gamma") {
		t.Fatalf("grep = %q", text)
	}
	if strings.Contains(text, "mac.txt:3:") {
		t.Fatalf("a lone CR split the file: %q", text)
	}
}
