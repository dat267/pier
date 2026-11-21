package interactive

import (
	"encoding/base64"
	"encoding/json"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), core/export-html/index.ts exportFromFile applies
// normalizePath to an explicit outputPath, including home-directory expansion.
func TestExportFromFileExpandsHomeOutputPath(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "hello"}}, StopReason: ai.StopStop,
	})

	got, err := ExportFromFile(manager.GetSessionFile(), "~/export.html", "dark")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "export.html")
	if got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expanded export missing: %v", err)
	}
}

// pi v1.4.0 (4a080252), core/export-html/index.ts exportSessionToHtml uses
// the same normalizePath rule for the live /export and RPC export_html paths.
func TestExportSessionToHTMLExpandsHomeOutputPath(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "hello"}}, StopReason: ai.StopStop,
	})
	session, err := coding.NewAgentSession(&coding.SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	session.Sessions = manager
	got, err := ExportSessionToHTML(session, "~/export.html", "dark")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "export.html")
	if got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expanded export missing: %v", err)
	}
}

// TestAnsiToHTML pins the converter against upstream ansi-to-html.ts
// semantics: SGR styling spans, 256-color and RGB extensions, resets, and
// HTML escaping of the payload text.
func TestAnsiToHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain escapes nothing", "hello", "hello"},
		{"escapes html", `a<b>&"c"`, "a&lt;b&gt;&amp;&quot;c&quot;"},
		{"bold", "\x1b[1mbold\x1b[0m", `<span style="font-weight:bold">bold</span>`},
		{"standard fg", "\x1b[31mred\x1b[0m", `<span style="color:#800000">red</span>`},
		{"bright fg", "\x1b[91mbright\x1b[0m", `<span style="color:#ff0000">bright</span>`},
		{"bg", "\x1b[41;37mrev\x1b[0m", `<span style="color:#c0c0c0;background-color:#800000">rev</span>`},
		{"256 color", "\x1b[38;5;208morange\x1b[0m", `<span style="color:#ff8700">orange</span>`},
		{"rgb", "\x1b[38;2;12;34;56mx\x1b[0m", `<span style="color:rgb(12,34,56)">x</span>`},
		{"dim italic underline", "\x1b[2;3;4ms\x1b[0m", `<span style="opacity:0.6;font-style:italic;text-decoration:underline">s</span>`},
		{"reset between", "\x1b[1ma\x1b[0mb", `<span style="font-weight:bold">a</span>b`},
		{"empty reset only", "\x1b[0m", ""},
	}
	for _, testCase := range cases {
		if got := AnsiToHTML(testCase.in); got != testCase.want {
			t.Errorf("%s: got %q want %q", testCase.name, got, testCase.want)
		}
	}
	if got := AnsiLinesToHTML([]string{"a", ""}); got != `<div class="ansi-line">a</div><div class="ansi-line">&nbsp;</div>` {
		t.Errorf("lines: %q", got)
	}
}

// pi v1.4.0 packages/coding-agent/src/modes/interactive/theme/theme-schema.ts
// accepts string export colors, packages/coding-agent/src/modes/interactive/theme/theme.ts
// returns them raw, and packages/coding-agent/src/core/export-html/index.ts
// inserts them into a style element. Guard that context.
func TestGenerateSessionHTMLEscapesCustomExportCSS(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)
	t.Cleanup(func() {
		SetCustomThemesDir("")
		SetRegisteredThemes(nil)
	})
	colors := make(map[string]string, len(requiredThemeColors))
	for _, key := range requiredThemeColors {
		colors[key] = "#123456"
	}
	document, err := json.Marshal(map[string]any{
		"name":   "hostile-export-color",
		"colors": colors,
		"export": map[string]string{
			"pageBg": `#123456</style><script>window.injected=1</script><style>`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hostile.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	got := GenerateSessionHTML(&exportSessionData{}, "hostile-export-color")
	if strings.Contains(got, `</style><script>window.injected=1</script>`) {
		t.Fatal("custom export color escaped the style element")
	}
}

// pi v1.4.0 packages/coding-agent/src/modes/interactive/theme/theme.ts
// catches invalid export color resolution instead of crashing the HTML export.
func TestGetThemeExportColorsInvalidVariableReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)
	t.Cleanup(func() {
		SetCustomThemesDir("")
		SetRegisteredThemes(nil)
	})
	colors := make(map[string]string, len(requiredThemeColors))
	for _, key := range requiredThemeColors {
		colors[key] = "#123456"
	}
	document, err := json.Marshal(map[string]any{
		"name":   "invalid-export-color",
		"colors": colors,
		"export": map[string]string{"pageBg": "missing-variable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "invalid.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	pageBg, cardBg, infoBg, err := GetThemeExportColors("invalid-export-color")
	if err != nil {
		t.Fatal(err)
	}
	if pageBg != "" || cardBg != "" || infoBg != "" {
		t.Fatalf("invalid export colors = %q %q %q, want all empty", pageBg, cardBg, infoBg)
	}
}

// pi v1.4.0 packages/coding-agent/src/modes/interactive/theme/theme-schema.ts
// rejects unknown color keys; packages/coding-agent/src/core/export-html/index.ts
// emits accepted keys as CSS vars.
func TestGenerateSessionHTMLRejectsUnknownThemeColorKey(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)
	t.Cleanup(func() {
		SetCustomThemesDir("")
		SetRegisteredThemes(nil)
	})
	colors := make(map[string]string, len(requiredThemeColors)+1)
	for _, key := range requiredThemeColors {
		colors[key] = "#123456"
	}
	colors[`bad</style><script>window.injected=2</script><style>`] = "#123456"
	document, err := json.Marshal(map[string]any{
		"name":   "unknown-color-key",
		"colors": colors,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unknown.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	got := GenerateSessionHTML(&exportSessionData{}, "unknown-color-key")
	if strings.Contains(got, `</style><script>window.injected=2</script>`) {
		t.Fatal("unknown theme color key escaped into the HTML style element")
	}
}

// TestExportSessionToHTML drives a real session file through the exporter
// and pins the document structure plus the embedded session payload.
func TestExportSessionToHTML(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(true)})
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	agentSession, sessionErr := coding.NewAgentSession(&coding.SessionConfig{
		Cwd: dir, Model: model, StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return nil
		},
	})
	if sessionErr != nil {
		t.Fatalf("session: %v", sessionErr)
	}
	agentSession.Sessions = manager
	userMessage := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "export me"}}
	manager.AppendMessage(userMessage)
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "here you go"}}, StopReason: ai.StopStop}
	manager.AppendMessage(assistant)

	session := &AppSession{AgentSession: agentSession}
	// Upstream's default output path is a relative file in process.cwd().
	defaultOutput, err := session.ExportSessionToHTML("", "dark")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	wantName := coding.AppName + "-session-" + strings.TrimSuffix(filepath.Base(manager.GetSessionFile()), ".jsonl") + ".html"
	if defaultOutput != wantName {
		t.Fatalf("output = %q want %q", defaultOutput, wantName)
	}
	os.Remove(defaultOutput)

	output := filepath.Join(dir, "export.html")
	if _, err := session.ExportSessionToHTML(output, "dark"); err != nil {
		t.Fatalf("export: %v", err)
	}
	html, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	document := string(html)
	for _, placeholder := range []string{"{{CSS}}", "{{JS}}", "{{SESSION_DATA}}", "{{MARKED_JS}}", "{{MARKED_CJK_FRIENDLY_JS}}", "{{HIGHLIGHT_JS}}", "{{THEME_VARS}}", "{{BODY_BG}}"} {
		if strings.Contains(document, placeholder) {
			t.Errorf("placeholder %s not substituted", placeholder)
		}
	}
	// JS String.replace expands `$` patterns in the replacement, and the pinned
	// bundles contain them, so the reference's own export loses bytes: its
	// highlight bundle's `$&` becomes the literal "{{HIGHLIGHT_JS}}" and its
	// footer `$${totalCost.toFixed(3)}` collapses to `${...}`, an undefined
	// identifier. Pier substitutes literally (D386).
	if !strings.Contains(document, `[-+*\/?!$&|:<=>@^~]`) {
		t.Error("export lost the highlight bundle's $& selector to replacement expansion")
	}
	if !strings.Contains(document, "$${totalCost.toFixed(3)}") {
		t.Error("export lost the footer's literal $${totalCost} to replacement expansion")
	}
	// The pinned export bundles Marked's CJK-friendly tokenizer override, which
	// its template.js applies to emStrong; without it exported emphasis next to
	// CJK text renders differently from the reference.
	if !strings.Contains(document, "marked-cjk-friendly v0.1.2") {
		t.Error("export document does not embed the CJK-friendly Marked bundle")
	}
	if !strings.Contains(document, "markedCjkFriendly().tokenizer.emStrong") {
		t.Error("export document does not apply the CJK-friendly emStrong override")
	}
	// Decode the embedded payload and check the session data round-trips.
	start := strings.Index(document, `<script id="session-data" type="application/json">`) + len(`<script id="session-data" type="application/json">`)
	end := strings.Index(document[start:], "</script>")
	payload, err := base64.StdEncoding.DecodeString(document[start : start+end])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var data struct {
		Header  json.RawMessage `json:"header"`
		Entries []struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"entries"`
		LeafID        *string                    `json:"leafId"`
		SystemPrompt  string                     `json:"systemPrompt"`
		RenderedTools map[string]json.RawMessage `json:"renderedTools"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if data.Header == nil || len(data.Entries) != 2 || data.LeafID == nil {
		t.Fatalf("payload shape: header=%v entries=%d leaf=%v", data.Header, len(data.Entries), data.LeafID)
	}
	if !strings.Contains(string(data.Entries[0].Message.Content), "export me") {
		t.Fatalf("user message missing: %s", data.Entries[0].Message.Content)
	}
	// Bash/read/write/edit/ls are template-rendered; nothing pre-rendered here.
	if len(data.RenderedTools) != 0 {
		t.Fatalf("renderedTools = %v", data.RenderedTools)
	}
}

// TestExportSessionToHTMLErrors pins the upstream error messages.
func TestExportSessionToHTMLErrors(t *testing.T) {
	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(true)})
	session := &AppSession{AgentSession: &coding.AgentSession{Sessions: manager}}
	if _, err := session.ExportSessionToHTML("", ""); err == nil || err.Error() != "Nothing to export yet - start a conversation first" {
		t.Fatalf("err = %v", err)
	}
	empty := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(false)})
	bare := &AppSession{AgentSession: &coding.AgentSession{Sessions: empty}}
	if _, err := bare.ExportSessionToHTML("", ""); err == nil || err.Error() != "Cannot export in-memory session to HTML" {
		t.Fatalf("err = %v", err)
	}
}

// TestExportHiddenMessageToggle pins b2bd111f2: the export template grows an H
// toggle for custom messages with display=false, and the entry carries
// display:false into the embedded payload. Upstream adds no test for this.
func TestExportHiddenMessageToggle(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(true)})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	manager.AppendCustomMessageEntry("preset-state", json.RawMessage(`"hidden payload"`), false, nil)

	agentSession, err := coding.NewAgentSession(&coding.SessionConfig{
		Cwd:   dir,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	agentSession.Sessions = manager

	output := filepath.Join(dir, "hidden.html")
	if _, err := (&AppSession{AgentSession: agentSession}).ExportSessionToHTML(output, "dark"); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	document := string(html)
	for _, marker := range []string{
		`data-action="toggle-hidden-messages"`,
		"hook-message-hidden",
		"show-hidden-messages",
		"H toggle hidden messages",
	} {
		if !strings.Contains(document, marker) {
			t.Errorf("export is missing %q", marker)
		}
	}
	if payload := decodeExportPayload(t, document); !strings.Contains(payload, `"display":false`) {
		t.Errorf("custom message display:false missing from payload: %s", payload)
	}
}

// TestExportFromFile drives a session file that this process never opened,
// which is what `pier --export <file>` does (upstream exportFromFile).
func TestExportFromFile(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(true)})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "export me"}})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "here you go"}}, StopReason: ai.StopStop,
	})
	sessionFile := manager.GetSessionFile()
	if sessionFile == "" {
		t.Fatal("no session file was written")
	}
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("session file: %v", err)
	}

	output := filepath.Join(dir, "from-file.html")
	got, err := ExportFromFile(sessionFile, output, "dark")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if got != output {
		t.Errorf("output = %q, want %q", got, output)
	}
	html, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The payload is embedded base64-encoded; decode it and check the entries
	// travelled, rather than matching the whole document.
	if !strings.Contains(string(html), "<!DOCTYPE html>") {
		t.Error("not an HTML document")
	}
	payload := decodeExportPayload(t, string(html))
	if !strings.Contains(payload, "export me") || !strings.Contains(payload, "here you go") {
		t.Errorf("the session messages are missing from the export payload: %s", payload)
	}
	// A file-based export carries no system prompt or tool set (the process
	// never had them), which upstream expresses as undefined.
	var data struct {
		SystemPrompt string            `json:"systemPrompt"`
		Tools        []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if data.SystemPrompt != "" || len(data.Tools) != 0 {
		t.Errorf("a file export should carry no prompt or tools: %s", payload)
	}
}

// decodeExportPayload pulls the base64 session payload out of an export
// document.
func decodeExportPayload(t *testing.T, document string) string {
	t.Helper()
	const tag = `<script id="session-data" type="application/json">`
	start := strings.Index(document, tag)
	if start < 0 {
		t.Fatalf("no session payload in the document")
	}
	start += len(tag)
	end := strings.Index(document[start:], "</script>")
	if end < 0 {
		t.Fatalf("unterminated session payload")
	}
	payload, err := base64.StdEncoding.DecodeString(document[start : start+end])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return string(payload)
}

// A file that does not exist is an error naming the resolved path, not a panic
// or an empty document (upstream throws "File not found: <path>").
func TestExportFromFileMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.jsonl")
	_, err := ExportFromFile(missing, "", "dark")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "File not found: ") || !strings.Contains(err.Error(), missing) {
		t.Errorf("err = %q, want it to name the resolved path", err)
	}
}

// An empty output path defaults to <app>-session-<basename>.html.
func TestExportFromFileDefaultOutputName(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(true)})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "hello"}}, StopReason: ai.StopStop,
	})
	sessionFile := manager.GetSessionFile()

	got, err := ExportFromFile(sessionFile, "", "dark")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer os.Remove(got)
	want := coding.AppName + "-session-" + strings.TrimSuffix(filepath.Base(sessionFile), ".jsonl") + ".html"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Upstream's live export always carries systemPrompt and tools (JSON.stringify
// keeps "" and []), while the standalone file export leaves them undefined.
func TestExportSessionDataShape(t *testing.T) {
	emptyPrompt := ""
	emptyTools := []exportTool{}
	live, err := ai.MarshalJSON(&exportSessionData{SystemPrompt: &emptyPrompt, Tools: &emptyTools})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `"systemPrompt":""`) || !strings.Contains(string(live), `"tools":[]`) {
		t.Fatalf("live = %s", live)
	}
	standalone, err := ai.MarshalJSON(&exportSessionData{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(standalone), "systemPrompt") || strings.Contains(string(standalone), "tools") {
		t.Fatalf("standalone = %s", standalone)
	}
}

// The RPC export_html seam calls the package function directly, without an
// AppSession wrapper (upstream agent-session.exportToHtml).
func TestExportSessionToHTMLPackageFunction(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: new(true)})
	agentSession, err := coding.NewAgentSession(&coding.SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"},
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	agentSession.Sessions = manager
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "export me"}})

	output := filepath.Join(dir, "rpc-export.html")
	path, err := ExportSessionToHTML(agentSession, output, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if path != output {
		t.Fatalf("path = %q want %q", path, output)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

// TestExportThemeVarsAreSorted pins D482. core/export-html/index.ts
// generateThemeVars walks Object.entries(getResolvedThemeColors(...)), so the
// reference writes the theme's own key order into the document: "customMessageLabel"
// comes before "customMessageBg" and "mdCode" before "dim". A Go map has no
// order, so the port sorts the names; the declarations are the same set and the
// order of CSS custom properties does not change the rendering.
func TestExportThemeVarsAreSorted(t *testing.T) {
	useBuiltinPierThemeForTest(t)

	vars := generateThemeVars("dark")
	declarations := strings.Split(vars, "\n      ")
	if len(declarations) < 4 {
		t.Fatalf("theme vars = %q", vars)
	}
	colors, err := GetResolvedThemeColors("dark")
	if err != nil {
		t.Fatalf("theme colors: %v", err)
	}

	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations[:len(declarations)-3] {
		name, _, ok := strings.Cut(strings.TrimPrefix(declaration, "--"), ": ")
		if !ok {
			t.Fatalf("bad declaration %q", declaration)
		}
		names = append(names, name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("theme vars are not sorted: %v", names)
	}
	if len(names) != len(colors) {
		t.Errorf("%d declarations for %d resolved colors", len(names), len(colors))
	}
	tail := declarations[len(declarations)-3:]
	if !strings.HasPrefix(tail[0], "--exportPageBg: ") || !strings.HasPrefix(tail[1], "--exportCardBg: ") ||
		!strings.HasPrefix(tail[2], "--exportInfoBg: ") {
		t.Errorf("export vars do not end the block: %v", tail)
	}
}

// TestExportWritePublishesAtomically pins A506: /export wrote its target with
// O_TRUNC, so exporting over an existing file destroyed that file before the new
// HTML was durable. The write now goes through coding.WritePrivateFile, which
// renames a fully written temp file into place.
func TestExportWritePublishesAtomically(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "notes.html")

	previous := []byte("<p>previous export</p>\n")
	if err := writePrivateFile(path, previous); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	// The successful path publishes the new content and keeps no debris.
	if err := writePrivateFile(path, []byte("<p>second export</p>\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "<p>second export</p>\n" {
		t.Fatalf("second write left %q (%v)", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.html" {
		t.Errorf("the export left debris beside its target: %v", entries)
	}

	// A write that cannot publish must fail without touching the target.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := writePrivateFile(path, []byte("<p>third export</p>\n")); err == nil {
		t.Fatalf("a write that cannot create its new file reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed export removed the target: %v", readErr)
	}
	if string(got) != "<p>second export</p>\n" {
		t.Errorf("the failed export left %q, want the previous content", got)
	}
}

// pi v1.4.0 (4a080252), core/export-html/index.ts generateHtml inserts a
// base64 JSON payload and the embedded assets into the HTML template. In Go,
// every replacement after that insertion copies the entire payload again.
func TestGenerateSessionHTMLBoundsLargePayloadAllocations(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	const size = 2 << 20
	prompt := strings.Repeat("x", size)
	data := &exportSessionData{SystemPrompt: &prompt}
	// Warm type/theme metadata, then measure both operations with cold pools
	// and no automatic GC. JSON allocation costs vary under race instrumentation
	// and are not the template-copy overhead this regression bounds.
	GenerateSessionHTML(data, "dark")
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	payload, err := ai.MarshalJSON(data)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	serializationBytes := after.TotalAlloc - before.TotalAlloc

	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	document := GenerateSessionHTML(data, "dark")
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	overhead := int64(allocated) - int64(serializationBytes)
	// Allow two base64 buffers, one final document, plus headroom for fixed
	// assets and a race-instrumented encoder pool miss (one 4 MiB buffer).
	// Not three additional payload-sized documents.
	budget := int64(3*base64.StdEncoding.EncodedLen(len(payload)) + 7<<20)
	t.Logf("2 MiB prompt: total=%d, serialization=%d, assembly overhead=%d", allocated, serializationBytes, overhead)
	if overhead > budget {
		t.Errorf("export assembly allocated %d bytes beyond serialization, want at most %d", overhead, budget)
	}

	// The whole document must match a small export with only its embedded
	// session data replaced. Asset insertion must not change payload bytes.
	empty := GenerateSessionHTML(&exportSessionData{}, "dark")
	oldPayload := decodeExportPayload(t, empty)
	want := strings.Replace(empty,
		base64.StdEncoding.EncodeToString([]byte(oldPayload)),
		base64.StdEncoding.EncodeToString(payload), 1)
	if document != want {
		t.Fatal("large export changed document or session payload bytes")
	}
}

// OSC 8 hyperlinks become real links in the exported HTML when the URL is http,
// https or mailto; other OSC/CSI/APC sequences and stray ESC characters are
// dropped (ansi-to-html.ts, v1.6.0, #12052).
func TestAnsiToHTMLConvertsOSC8Hyperlinks(t *testing.T) {
	link := "\x1b]8;;https://example.test/a?b=1&c=2\x07label\x1b]8;;\x07"
	if got := AnsiToHTML(link); got != `<a href="https://example.test/a?b=1&amp;c=2">label</a>` {
		t.Fatalf("osc8 link = %q", got)
	}
	// mailto keeps its URL, other schemes lose the link but keep the text.
	if got := AnsiToHTML("\x1b]8;;mailto:dev@example.test\x1b\\post\x1b]8;;\x1b\\"); got != `<a href="mailto:dev@example.test">post</a>` {
		t.Fatalf("mailto link = %q", got)
	}
	if got := AnsiToHTML("\x1b]8;;javascript:alert(1)\x07bad\x1b]8;;\x07"); got != "bad" {
		t.Fatalf("unsafe link = %q, want the text only", got)
	}
	// Other escape sequences and stray ESC bytes do not reach the HTML.
	got := AnsiToHTML("a\x1b[2Kb\x1b]0;title\x07c\x1b_Payload\x1b\\d\x1be")
	if got != "abcde" {
		t.Fatalf("other sequences = %q, want them dropped", got)
	}
	// Style spans nest inside a link.
	styled := AnsiToHTML("\x1b]8;;https://example.test\x07\x1b[31mred\x1b[0m\x1b]8;;\x07")
	if !strings.Contains(styled, `<a href="https://example.test">`) || !strings.Contains(styled, ">red</span>") {
		t.Fatalf("styled link = %q", styled)
	}
}
