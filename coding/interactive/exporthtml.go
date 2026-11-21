package interactive

// Port of packages/coding-agent/src/core/export-html (index.ts and
// ansi-to-html.ts): session HTML export for /export. The templates are the
// upstream v0.99.1 files, embedded verbatim. Upstream pre-renders only tools
// whose definitions carry TUI renderers — stock tools have none (verified:
// createGrepTool exposes no renderCall), so without extension renderers
// (D41/D133) renderedTools is always omitted and the template's structured
// fallback draws every tool, exactly like upstream. The template's nestedCalls
// rendering (the renderNestedCalls block, from the codemode work) stays inert:
// the port has no codemode, so no tool result carries that field.

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

//go:embed exporthtml/template.html
var exportTemplateHTML string

//go:embed exporthtml/template.css
var exportTemplateCSS string

//go:embed exporthtml/template.js
var exportTemplateJS string

//go:embed exporthtml/vendor/marked.min.js
var exportMarkedJS string

//go:embed exporthtml/vendor/marked-cjk-friendly.min.js
var exportMarkedCJKFriendlyJS string

//go:embed exporthtml/vendor/highlight.min.js
var exportHighlightJS string

// ---- ANSI to HTML (ansi-to-html.ts) ----

var ansiColors = [16]string{
	"#000000", "#800000", "#008000", "#808000",
	"#000080", "#800080", "#008080", "#c0c0c0",
	"#808080", "#ff0000", "#00ff00", "#ffff00",
	"#0000ff", "#ff00ff", "#00ffff", "#ffffff",
}

var ansiSequenceRegex = regexp.MustCompile(
	"\x1b\\[([0-9;]*)m" + // SGR
		"|\x1b\\[[0-?]*[ -/]*[@-~]" + // other CSI
		"|\x1b\\]([^\x07\x1b]*)(?:\x07|\x1b\\\\)" + // OSC, BEL or ST terminated
		"|\x1b_[^\x07\x1b]*(?:\x07|\x1b\\\\)") // APC

// osc8SequenceRegex matches an OSC 8 payload: "8;params;url".
var osc8SequenceRegex = regexp.MustCompile(`^8;[^;]*;(?s:(.*))$`)

// safeLinkRegex is the URL scheme an exported link may carry.
var safeLinkRegex = regexp.MustCompile(`(?i)^(?:https?:|mailto:)`)

// stripAnsiEscapes removes every escape sequence the converter drops, plus stray
// ESC characters (ansi-to-html.ts stripAnsiEscapes).
func stripAnsiEscapes(text string) string {
	return strings.ReplaceAll(ansiSequenceRegex.ReplaceAllString(text, ""), "\x1b", "")
}

func color256ToHex(index int) string {
	if index < 16 {
		return ansiColors[index]
	}
	if index < 232 {
		cubeIndex := index - 16
		r := cubeIndex / 36
		g := (cubeIndex % 36) / 6
		b := cubeIndex % 6
		component := func(n int) string {
			value := 0
			if n != 0 {
				value = 55 + n*40
			}
			return fmt.Sprintf("%02x", value)
		}
		return "#" + component(r) + component(g) + component(b)
	}
	gray := 8 + (index-232)*10
	return fmt.Sprintf("#%02x%02x%02x", gray, gray, gray)
}

func escapeHTMLText(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#039;",
	)
	return replacer.Replace(text)
}

type htmlTextStyle struct {
	fg, bg            string
	bold, dim         bool
	italic, underline bool
}

func styleToInlineCSS(style *htmlTextStyle) string {
	parts := make([]string, 0, 6)
	if style.fg != "" {
		parts = append(parts, "color:"+style.fg)
	}
	if style.bg != "" {
		parts = append(parts, "background-color:"+style.bg)
	}
	if style.bold {
		parts = append(parts, "font-weight:bold")
	}
	if style.dim {
		parts = append(parts, "opacity:0.6")
	}
	if style.italic {
		parts = append(parts, "font-style:italic")
	}
	if style.underline {
		parts = append(parts, "text-decoration:underline")
	}
	return strings.Join(parts, ";")
}

func hasHTMLStyle(style *htmlTextStyle) bool {
	return style.fg != "" || style.bg != "" || style.bold || style.dim || style.italic || style.underline
}

// maxSGRParameter clamps an SGR field that is wider than an int. It sits well
// past every code the parser reads, so a clamped field falls through every
// branch, which is what the huge Number upstream's `parseInt(p, 10) || 0`
// produces for it. Empty and non-numeric fields are 0, matching `|| 0`.
const maxSGRParameter = 1 << 20

// ansiSGRParameter parses one SGR field. Clamping instead of wrapping matters:
// a wrapped value could land on 0, the reset code, which dropped the styling of
// the rest of the line (A435).
func ansiSGRParameter(part string) int {
	value := 0
	for index := 0; index < len(part); index++ {
		digit := part[index]
		if digit < '0' || digit > '9' {
			return 0
		}
		if value < maxSGRParameter {
			value = value*10 + int(digit-'0')
			if value > maxSGRParameter {
				value = maxSGRParameter
			}
		}
	}
	return value
}

func applySGRCode(params []int, style *htmlTextStyle) {
	for i := 0; i < len(params); i++ {
		code := params[i]
		switch {
		case code == 0:
			*style = htmlTextStyle{}
		case code == 1:
			style.bold = true
		case code == 2:
			style.dim = true
		case code == 3:
			style.italic = true
		case code == 4:
			style.underline = true
		case code == 22:
			style.bold = false
			style.dim = false
		case code == 23:
			style.italic = false
		case code == 24:
			style.underline = false
		case code >= 30 && code <= 37:
			style.fg = ansiColors[code-30]
		case code == 38:
			if i+2 < len(params) && params[i+1] == 5 {
				style.fg = color256ToHex(params[i+2])
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				style.fg = "rgb(" + strconv.Itoa(params[i+2]) + "," + strconv.Itoa(params[i+3]) + "," + strconv.Itoa(params[i+4]) + ")"
				i += 4
			}
		case code == 39:
			style.fg = ""
		case code >= 40 && code <= 47:
			style.bg = ansiColors[code-40]
		case code == 48:
			if i+2 < len(params) && params[i+1] == 5 {
				style.bg = color256ToHex(params[i+2])
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				style.bg = "rgb(" + strconv.Itoa(params[i+2]) + "," + strconv.Itoa(params[i+3]) + "," + strconv.Itoa(params[i+4]) + ")"
				i += 4
			}
		case code == 49:
			style.bg = ""
		case code >= 90 && code <= 97:
			style.fg = ansiColors[code-90+8]
		case code >= 100 && code <= 107:
			style.bg = ansiColors[code-100+8]
		}
	}
}

// AnsiToHTML converts ANSI-escaped text to HTML with inline styles
// (upstream ansiToHtml).
func AnsiToHTML(text string) string {
	style := &htmlTextStyle{}
	var result strings.Builder
	lastIndex := 0
	inSpan := false
	inLink := false
	closeSpan := func() {
		if inSpan {
			result.WriteString("</span>")
			inSpan = false
		}
	}
	openSpan := func() {
		if hasHTMLStyle(style) {
			result.WriteString(`<span style="` + styleToInlineCSS(style) + `">`)
			inSpan = true
		}
	}

	for _, loc := range ansiSequenceRegex.FindAllStringSubmatchIndex(text, -1) {
		if before := text[lastIndex:loc[0]]; before != "" {
			result.WriteString(escapeHTMLText(stripStrayEscapes(before)))
		}
		switch {
		case loc[2] >= 0:
			// SGR: re-style the following text.
			paramStr := text[loc[2]:loc[3]]
			params := []int{0}
			if paramStr != "" {
				params = params[:0]
				for part := range strings.SplitSeq(paramStr, ";") {
					params = append(params, ansiSGRParameter(part))
				}
			}
			closeSpan()
			applySGRCode(params, style)
			openSpan()
		case loc[4] >= 0:
			// OSC: only an OSC 8 hyperlink survives, and only with a safe scheme.
			if hyperlink := osc8SequenceRegex.FindStringSubmatch(text[loc[4]:loc[5]]); hyperlink != nil {
				// Browsers strip C0 controls from URLs, so strip them before
				// trimming; the other order leaves a control-then-spaces URL
				// with leading spaces and fails the scheme check
				// (ansi-to-html.ts).
				url := strings.TrimSpace(stripControlBytes(hyperlink[1]))
				closeSpan()
				if inLink {
					result.WriteString("</a>")
					inLink = false
				}
				if url != "" && safeLinkRegex.MatchString(url) {
					result.WriteString(`<a href="` + escapeHTMLText(url) + `">`)
					inLink = true
				}
				openSpan()
			}
		}
		// Other CSI and APC sequences are dropped.
		lastIndex = loc[1]
	}
	if remaining := text[lastIndex:]; remaining != "" {
		result.WriteString(escapeHTMLText(stripStrayEscapes(remaining)))
	}
	closeSpan()
	if inLink {
		result.WriteString("</a>")
	}
	return result.String()
}

// stripStrayEscapes drops ESC bytes the sequence regex did not consume.
func stripStrayEscapes(text string) string { return strings.ReplaceAll(text, "\x1b", "") }

// stripControlBytes removes the C0 controls and DEL a browser would drop from a
// URL anyway, so the scheme check sees what the browser will use. It must run
// before the URL is trimmed: a control byte followed by spaces would otherwise
// keep those spaces and fail the scheme check.
func stripControlBytes(url string) string {
	var cleaned strings.Builder
	cleaned.Grow(len(url))
	for _, r := range url {
		if r < 0x20 || r == 0x7f {
			continue
		}
		cleaned.WriteRune(r)
	}
	return cleaned.String()
}

// AnsiLinesToHTML converts ANSI lines to wrapped div elements
// (upstream ansiLinesToHtml).
func AnsiLinesToHTML(lines []string) string {
	var result strings.Builder
	for _, line := range lines {
		html := AnsiToHTML(line)
		if html == "" {
			html = "&nbsp;"
		}
		result.WriteString(`<div class="ansi-line">` + html + `</div>`)
	}
	return result.String()
}

// ---- Export color derivation (index.ts) ----

// exportHexColorRegex and exportRGBColorRegex match the CSS colour forms the
// export stylesheet uses.
var (
	exportHexColorRegex = regexp.MustCompile(`^#([0-9a-fA-F]{2})([0-9a-fA-F]{2})([0-9a-fA-F]{2})$`)
	exportRGBColorRegex = regexp.MustCompile(`^rgb\s*\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)$`)
)

func parseExportColor(color string) (r, g, b int, ok bool) {
	hex := exportHexColorRegex.FindStringSubmatch(color)
	if hex != nil {
		values := make([]int, 3)
		for i := range 3 {
			value, err := strconv.ParseInt(hex[i+1], 16, 32)
			if err != nil {
				return 0, 0, 0, false
			}
			values[i] = int(value)
		}
		return values[0], values[1], values[2], true
	}
	rgb := exportRGBColorRegex.FindStringSubmatch(color)
	if rgb != nil {
		values := make([]int, 3)
		for i := range 3 {
			value, err := strconv.Atoi(rgb[i+1])
			if err != nil {
				return 0, 0, 0, false
			}
			values[i] = value
		}
		return values[0], values[1], values[2], true
	}
	return 0, 0, 0, false
}

func luminance(r, g, b int) float64 {
	toLinear := func(c int) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*toLinear(r) + 0.7152*toLinear(g) + 0.0722*toLinear(b)
}

func adjustBrightness(color string, factor float64) string {
	r, g, b, ok := parseExportColor(color)
	if !ok {
		return color
	}
	adjust := func(c int) int {
		value := int(math.Round(float64(c) * factor))
		return min(max(value, 0), 255)
	}
	return "rgb(" + strconv.Itoa(adjust(r)) + ", " + strconv.Itoa(adjust(g)) + ", " + strconv.Itoa(adjust(b)) + ")"
}

type exportColors struct {
	pageBg, cardBg, infoBg string
}

func deriveExportColors(baseColor string) exportColors {
	r, g, b, ok := parseExportColor(baseColor)
	if !ok {
		return exportColors{pageBg: "rgb(24, 24, 30)", cardBg: "rgb(30, 30, 36)", infoBg: "rgb(60, 55, 40)"}
	}
	clamp := func(value int) int { return min(max(value, 0), 255) }
	if luminance(r, g, b) > 0.5 {
		return exportColors{
			pageBg: adjustBrightness(baseColor, 0.96),
			cardBg: baseColor,
			infoBg: "rgb(" + strconv.Itoa(clamp(r+10)) + ", " + strconv.Itoa(clamp(g+5)) + ", " + strconv.Itoa(clamp(b-20)) + ")",
		}
	}
	return exportColors{
		pageBg: adjustBrightness(baseColor, 0.7),
		cardBg: adjustBrightness(baseColor, 0.85),
		infoBg: "rgb(" + strconv.Itoa(clamp(r+20)) + ", " + strconv.Itoa(clamp(g+15)) + ", " + strconv.Itoa(b) + ")",
	}
}

func generateThemeVars(themeName string) string {
	colors, err := GetResolvedThemeColors(themeName)
	if err != nil {
		colors = map[string]string{}
	}
	keys := make([]string, 0, len(colors))
	for key := range colors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys)+3)
	for _, key := range keys {
		lines = append(lines, "--"+key+": "+colors[key]+";")
	}

	exportPageBg, exportCardBg, exportInfoBg, _ := GetThemeExportColors(themeName)
	themeExport := exportColors{pageBg: exportPageBg, cardBg: exportCardBg, infoBg: exportInfoBg}
	userMessageBg := colors["userMessageBg"]
	if userMessageBg == "" {
		userMessageBg = "#343541"
	}
	derived := deriveExportColors(userMessageBg)
	pageBg, cardBg, infoBg := derived.pageBg, derived.cardBg, derived.infoBg
	if themeExport.pageBg != "" {
		pageBg = themeExport.pageBg
	}
	if themeExport.cardBg != "" {
		cardBg = themeExport.cardBg
	}
	if themeExport.infoBg != "" {
		infoBg = themeExport.infoBg
	}
	lines = append(lines, "--exportPageBg: "+pageBg+";", "--exportCardBg: "+cardBg+";", "--exportInfoBg: "+infoBg+";")
	return strings.Join(lines, "\n      ")
}

// ---- Session data + rendering (index.ts) ----

// renderedToolHTML is the pre-rendered HTML for one tool call/result
// (upstream RenderedToolHtml).
type renderedToolHTML struct {
	CallHTML            string `json:"callHtml,omitempty"`
	ResultHTMLCollapsed string `json:"resultHtmlCollapsed,omitempty"`
	ResultHTMLExpanded  string `json:"resultHtmlExpanded,omitempty"`
}

type exportTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type exportSessionData struct {
	// Header marshals with the leading "type":"session" discriminator, like
	// upstream's header entry object.
	Header  json.RawMessage       `json:"header"`
	Entries []coding.SessionEntry `json:"entries"`
	LeafID  *string               `json:"leafId"`
	// SystemPrompt and Tools are pointers so the live export can carry an empty
	// value (upstream JSON.stringify keeps "" and []) while the standalone file
	// export omits them (undefined).
	SystemPrompt  *string                      `json:"systemPrompt,omitempty"`
	Tools         *[]exportTool                `json:"tools,omitempty"`
	RenderedTools map[string]*renderedToolHTML `json:"renderedTools,omitempty"`
}

// GenerateSessionHTML renders the export document (upstream generateHtml).
func GenerateSessionHTML(data *exportSessionData, themeName string) string {
	prefix, suffix := sessionHTMLParts(themeName)
	payload, err := ai.MarshalJSON(data)
	if err != nil {
		payload = []byte("{}")
	}
	// One buffer, sized for the finished document: the payload is the largest
	// member of a real export, and growing a builder into it copies the page
	// again.
	var builder strings.Builder
	builder.Grow(len(prefix) + base64.StdEncoding.EncodedLen(len(payload)) + len(suffix))
	builder.WriteString(prefix)
	encoder := base64.NewEncoder(base64.StdEncoding, &builder)
	_, _ = encoder.Write(payload)
	_ = encoder.Close()
	builder.WriteString(suffix)
	return builder.String()
}

// WriteSessionHTML streams the export document to w: the payload is base64
// encoded straight into the file, and the entries arrive one at a time, so a
// large export never holds its own document or the whole session beside itself
// (D489). entries may be nil, which uses data's slice.
func WriteSessionHTML(w io.Writer, data *exportSessionData, entries func(yield func(coding.SessionEntry) error) error, themeName string) error {
	prefix, suffix := sessionHTMLParts(themeName)
	if _, err := io.WriteString(w, prefix); err != nil {
		return err
	}
	encoder := base64.NewEncoder(base64.StdEncoding, w)
	if err := writeExportPayload(encoder, data, entries); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	_, err := io.WriteString(w, suffix)
	return err
}

// writeExportPayload writes data's JSON members in the struct's order, yielding
// entries instead of marshalling the slice: on a real session the entries are
// nearly the whole document, so one slice marshal would hold the entries and
// their encoded copy at the same time.
func writeExportPayload(w io.Writer, data *exportSessionData, entries func(yield func(coding.SessionEntry) error) error) error {
	writeJSON := func(value any) error {
		encoded, err := ai.MarshalJSON(value)
		if err != nil {
			return err
		}
		_, err = w.Write(encoded)
		return err
	}
	if _, err := io.WriteString(w, `{"header":`); err != nil {
		return err
	}
	header := data.Header
	if len(header) == 0 {
		header = json.RawMessage("null")
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"entries":[`); err != nil {
		return err
	}
	first := true
	emit := func(entry coding.SessionEntry) error {
		if !first {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		first = false
		// A value would skip SessionEntry's pointer-receiver MarshalJSON and
		// lose the entry's retained line (its member order included).
		return writeJSON(&entry)
	}
	if entries != nil {
		if err := entries(emit); err != nil {
			return err
		}
	} else {
		for _, entry := range data.Entries {
			if err := emit(entry); err != nil {
				return err
			}
		}
	}
	if _, err := io.WriteString(w, `],"leafId":`); err != nil {
		return err
	}
	if err := writeJSON(data.LeafID); err != nil {
		return err
	}
	if data.SystemPrompt != nil {
		if _, err := io.WriteString(w, `,"systemPrompt":`); err != nil {
			return err
		}
		if err := writeJSON(*data.SystemPrompt); err != nil {
			return err
		}
	}
	if data.Tools != nil {
		if _, err := io.WriteString(w, `,"tools":`); err != nil {
			return err
		}
		if err := writeJSON(*data.Tools); err != nil {
			return err
		}
	}
	if data.RenderedTools != nil {
		if _, err := io.WriteString(w, `,"renderedTools":`); err != nil {
			return err
		}
		if err := writeJSON(data.RenderedTools); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

// sessionHTMLParts renders the template with every asset substituted and splits
// it at the session payload, the one placeholder left for the streaming write.
func sessionHTMLParts(themeName string) (string, string) {
	themeVars := generateThemeVars(themeName)
	colors, _ := GetResolvedThemeColors(themeName)
	exportPageBg, exportCardBg, exportInfoBg, _ := GetThemeExportColors(themeName)
	themeExport := exportColors{pageBg: exportPageBg, cardBg: exportCardBg, infoBg: exportInfoBg}
	userMessageBg := colors["userMessageBg"]
	if userMessageBg == "" {
		userMessageBg = "#343541"
	}
	derived := deriveExportColors(userMessageBg)
	bodyBg, containerBg, infoBg := derived.pageBg, derived.cardBg, derived.infoBg
	if themeExport.pageBg != "" {
		bodyBg = themeExport.pageBg
	}
	if themeExport.cardBg != "" {
		containerBg = themeExport.cardBg
	}
	if themeExport.infoBg != "" {
		infoBg = themeExport.infoBg
	}

	css := strings.Replace(exportTemplateCSS, "{{THEME_VARS}}", themeVars, 1)
	css = strings.Replace(css, "{{BODY_BG}}", bodyBg, 1)
	css = strings.Replace(css, "{{CONTAINER_BG}}", containerBg, 1)
	css = strings.Replace(css, "{{INFO_BG}}", infoBg, 1)

	html := strings.Replace(exportTemplateHTML, "{{CSS}}", css, 1)
	html = strings.Replace(html, "{{JS}}", exportTemplateJS, 1)
	html = strings.Replace(html, "{{MARKED_JS}}", exportMarkedJS, 1)
	html = strings.Replace(html, "{{MARKED_CJK_FRIENDLY_JS}}", exportMarkedCJKFriendlyJS, 1)
	html = strings.Replace(html, "{{HIGHLIGHT_JS}}", exportHighlightJS, 1)
	// The payload placeholder is the split point: the base64 data is streamed
	// between the halves instead of being substituted into a copy of the page.
	prefix, suffix, found := strings.Cut(html, "{{SESSION_DATA}}")
	if !found {
		return html, ""
	}
	return prefix, suffix
}

// ExportFromFile exports a session file to HTML (upstream exportFromFile). An
// empty outputPath defaults to "<app>-session-<basename>.html" in the working
// directory, and an empty themeName uses the current/default theme.
//
// Unlike ExportSessionToHTML this reads a session that this process never
// opened, so the exported document carries no system prompt or tool set —
// upstream reads the same fields from the file's header and entries.
func ExportFromFile(inputPath string, outputPath string, themeName string) (string, error) {
	resolvedInput := coding.ResolvePath(inputPath, "", coding.PathInputOptions{})
	if _, err := os.Stat(resolvedInput); err != nil {
		return "", fmt.Errorf("File not found: %s", resolvedInput)
	}
	sessionManager, err := coding.OpenSession(resolvedInput, "", "")
	if err != nil {
		return "", err
	}

	// The entries stream from the manager as the document is written; keeping a
	// snapshot here would hold the session's whole payload for the export.
	data := &exportSessionData{LeafID: sessionManager.GetLeafID()}
	if header := sessionManager.GetHeader(); header != nil {
		data.Header = exportHeaderJSON(header)
	}

	outputPath = coding.NormalizePath(outputPath, coding.PathInputOptions{})
	if outputPath == "" {
		base := strings.TrimSuffix(filepath.Base(resolvedInput), ".jsonl")
		outputPath = coding.AppName + "-session-" + base + ".html"
	}
	if err := writePrivateFileStream(outputPath, func(w io.Writer) error {
		return WriteSessionHTML(w, data, sessionManager.EachEntry, themeName)
	}); err != nil {
		return "", err
	}
	return outputPath, nil
}

// exportHeaderJSON wraps the session header in an entry carrying the "session"
// discriminator, the shape the export document's header field expects.
func exportHeaderJSON(header *coding.SessionHeader) json.RawMessage {
	headerJSON, err := ai.MarshalJSON(struct {
		Type string `json:"type"`
		*coding.SessionHeader
	}{Type: "session", SessionHeader: header})
	if err != nil {
		return json.RawMessage("null")
	}
	return headerJSON
}

// ExportSessionToHTML exports the current session branch to an HTML file
// (upstream exportSessionToHtml, via agent-session.exportToHtml). An empty
// themeName uses the currently applied theme. This is a package function so
// non-interactive hosts (the RPC runner) can inject it as a seam; the
// interactive AppSession method delegates to it.
func ExportSessionToHTML(session *coding.AgentSession, outputPath string, themeName string) (string, error) {
	sessionManager := session.Sessions
	sessionFile := sessionManager.GetSessionFile()
	if sessionFile == "" {
		return "", fmt.Errorf("Cannot export in-memory session to HTML")
	}
	if _, err := os.Stat(sessionFile); err != nil {
		return "", fmt.Errorf("Nothing to export yet - start a conversation first")
	}

	state := session.Agent.State()
	tools := make([]exportTool, 0, len(state.Tools))
	for _, tool := range state.Tools {
		tools = append(tools, exportTool{
			Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
		})
	}

	data := &exportSessionData{
		LeafID:       sessionManager.GetLeafID(),
		SystemPrompt: &state.SystemPrompt,
		Tools:        &tools,
	}
	if header := sessionManager.GetHeader(); header != nil {
		data.Header = exportHeaderJSON(header)
	}
	if themeName == "" {
		themeName = CurrentThemeName()
	}
	// A tool whose card the page's template cannot draw (an MCP tool) is
	// pre-rendered from the transcript's own renderer
	// (export-html/tool-renderer.ts).
	if theme := exportRenderTheme(); theme != nil {
		data.RenderedTools = renderToolHTMLForExport(session, theme, session.Cwd)
	}

	outputPath = coding.NormalizePath(outputPath, coding.PathInputOptions{})
	if outputPath == "" {
		base := filepath.Base(sessionFile)
		base = strings.TrimSuffix(base, ".jsonl")
		outputPath = coding.AppName + "-session-" + base + ".html"
	}
	if err := writePrivateFileStream(outputPath, func(w io.Writer) error {
		return WriteSessionHTML(w, data, sessionManager.EachEntry, themeName)
	}); err != nil {
		return "", err
	}
	return outputPath, nil
}

// writePrivateFileStream publishes the export through the shared atomic
// replace, so a failed export cannot truncate a file the user already had, and
// the document is written as it is produced instead of being held whole (A506,
// D454, D489).
func writePrivateFile(path string, data []byte) error {
	return coding.WritePrivateFile(path, data)
}

func writePrivateFileStream(path string, write func(io.Writer) error) error {
	return coding.WritePrivateFileStream(path, write)
}

// ExportSessionToHTML exports the current session branch to an HTML file
// (upstream exportSessionToHtml, via agent-session.exportToHtml). An empty
// themeName uses the currently applied theme.
func (s *AppSession) ExportSessionToHTML(outputPath string, themeName string) (string, error) {
	return ExportSessionToHTML(s.AgentSession, outputPath, themeName)
}
