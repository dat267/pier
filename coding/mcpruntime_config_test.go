package coding

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
)

// Port of the "MCP connections" cases of upstream
// packages/coding-agent/test/mcp-extension.test.ts, plus the tool definition
// from createMcpToolDefinition.

// TestMcpConfigValueAndHomeExpansion covers upstream's shell-style config
// values (`${NAME}`, `!cmd`) and `~` expansion.
func TestMcpConfigValueAndHomeExpansion(t *testing.T) {
	t.Setenv("MCP_TEST_TOKEN", "secret")
	if got, err := ResolveMcpConfigValue("Bearer ${MCP_TEST_TOKEN}"); err != nil || got != "Bearer secret" {
		t.Fatalf("value = %q err = %v", got, err)
	}
	if got, err := ResolveMcpConfigValue("${MCP_TEST_MISSING}"); err != nil || got != "" {
		t.Fatalf("missing env = %q err = %v", got, err)
	}
	output, err := ResolveMcpConfigValue("!printf hello")
	if err != nil {
		t.Fatal(err)
	}
	if output != "hello" {
		t.Fatalf("command = %q", output)
	}
	if output, err := ResolveMcpConfigValue(`!printf '\377'`); err != nil || output != "\uFFFD" {
		t.Fatalf("malformed UTF-8 command output = %q, %v", output, err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := expandMcpHome("~/work"); got != home+"/work" {
		t.Fatalf("home = %q", got)
	}
	if got := expandMcpHome("plain"); got != "plain" {
		t.Fatalf("plain = %q", got)
	}
	if got := expandMcpHome("~"); got != home {
		t.Fatalf("tilde = %q", got)
	}
}

// Upstream parseConfigValueTemplate handles "$$"/"$!" escapes and only treats a
// value starting with "!" (no trimming) as a command.
func TestResolveMcpConfigValueEscapes(t *testing.T) {
	if got, _ := ResolveMcpConfigValue("pa$$word"); got != "pa$word" {
		t.Fatalf("$$ = %q; want pa$word", got)
	}
	if got, _ := ResolveMcpConfigValue("$!bang"); got != "!bang" {
		t.Fatalf("$! = %q; want !bang", got)
	}
	if got, _ := ResolveMcpConfigValue(" !printf hi"); got != " !printf hi" {
		t.Fatalf("leading space should not run a command: %q", got)
	}
}

// runtime.ts keeps the last 2000 UTF-16 units of the JavaScript-trimmed
// stderr; slicing bytes split multi-byte characters.
func TestMcpStderrTailUsesJavaScriptStringUnits(t *testing.T) {
	tail := truncateMcpStderr(strings.Repeat("界", 2500))
	if got := ai.JSLength(tail); got != mcpStderrTailChars {
		t.Fatalf("tail units = %d; want %d", got, mcpStderrTailChars)
	}
	if !utf8.ValidString(tail) {
		t.Fatal("tail is not valid UTF-8; a byte slice split a character")
	}
	if got := truncateMcpStderr("\uFEFFerror"); got != "error" {
		t.Fatalf("tail = %q; want JavaScript trim to strip the BOM", got)
	}
}

// pi v1.5.2 (b4afdddf) extensions/mcp/runtime.ts expandHome also expands a
// Windows home reference, `~\…`, which the port only did for `~/…`.
func TestExpandMcpHomeHandlesWindowsSeparator(t *testing.T) {
	home := t.TempDir()
	if got, want := expandMcpHomeOn(`~\bin\server.exe`, home, "windows"), filepath.Join(home, `bin\server.exe`); got != want {
		t.Fatalf(`~\ = %q, want %q`, got, want)
	}
	if got := expandMcpHomeOn(`~\bin`, home, "linux"); got != `~\bin` {
		t.Fatalf("linux kept the Windows form: %q", got)
	}
	if got, want := expandMcpHomeOn("~/work", home, "windows"), filepath.Join(home, "work"); got != want {
		t.Fatalf("~/ on windows = %q, want %q", got, want)
	}
}
