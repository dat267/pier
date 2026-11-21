package coding

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Port of core/tools/find.ts. The default implementation shells out to `fd`
// (upstream ensureTool downloads it when missing — the download manager is
// host surface and unported; PATH lookup is the Go equivalent).

const findDefaultLimit = 1000

// FindToolDescription is upstream's find description.
const FindToolDescription = "Search for files by glob pattern. Returns matching file paths relative to the search directory. Respects .gitignore. Output is truncated to 1000 results or 50KB (whichever is hit first)."

// FindToolDetails carries find truncation info.
type FindToolDetails struct {
	Truncation         *TruncationResult `json:"truncation,omitempty"`
	ResultLimitReached *float64          `json:"resultLimitReached,omitempty"`
}

var findSchemaJSON = mustMarshalJSON(json.RawMessage(`{
	"type": "object",
	"required": ["pattern"],
	"properties": {
		"pattern": {"type": "string", "description": "Glob pattern relative to the search directory, e.g. 'src/**/*.ts'. Without '/', e.g. '*.ts', it matches file names at any depth"},
		"path": {"type": "string", "description": "Directory to search in (default: current directory)"},
		"limit": {"type": "number", "description": "Maximum number of results (default: 1000)"}
	}
}`))

// RelativizeFindResultPath relativizes a find result against the search
// root and normalizes to posix separators.
func RelativizeFindResultPath(resultPath string, searchPath string) string {
	separator := string(os.PathSeparator)
	hadTrailingSeparator := strings.HasSuffix(resultPath, separator) ||
		(separator == "\\" && strings.HasSuffix(resultPath, "/"))
	relativePath := resultPath
	if filepath.IsAbs(resultPath) {
		if rel, err := filepath.Rel(searchPath, resultPath); err == nil {
			relativePath = rel
		}
	}
	posixPath := filepath.ToSlash(relativePath)
	if hadTrailingSeparator && !strings.HasSuffix(posixPath, "/") {
		return posixPath + "/"
	}
	return posixPath
}

// findFullPathVariable prepares the fd pattern (port of find.ts v1.6.0):
//   - fd matches the basename unless --full-path, which a pattern containing "/"
//     needs;
//   - a relative path pattern is anchored at the search directory, so "src/*.ts"
//     finds "src/a.ts" but not "pkg/src/b.ts" (an unanchored search is written
//     "**/src/*.ts"), which means prefixing the glob-escaped search path because
//     fd matches the absolute candidate path;
//   - a leading "./" is dropped, since fd's full paths never contain "/./";
//   - smart case follows the user's pattern, so it is decided here and passed as
//     --ignore-case instead of by the search path's own letters;
//   - on Windows each "/" becomes a [/\\] class, because fd matches native
//     separators against the absolute path.
func findFullPathVariable(pattern, searchPath, goos string) (effectivePattern string, fullPath bool, ignoreCase bool) {
	effectivePattern = pattern
	if goos == "windows" {
		effectivePattern = strings.ReplaceAll(effectivePattern, `\`, "/")
	}
	for strings.HasPrefix(effectivePattern, "./") {
		effectivePattern = effectivePattern[2:]
	}
	if !strings.Contains(effectivePattern, "/") {
		return effectivePattern, false, false
	}
	ignoreCase = effectivePattern == strings.ToLower(effectivePattern)
	if !isAbsoluteGlobPattern(effectivePattern) {
		// The escape is upstream's non-Windows branch; on Windows the search path
		// keeps its native separators and each "/" becomes a separator class below.
		root := searchPath
		if goos != "windows" {
			root = escapeGlobMeta(root)
		}
		effectivePattern = strings.TrimRight(root, "/") + "/" + effectivePattern
	}
	if goos == "windows" {
		effectivePattern = strings.ReplaceAll(effectivePattern, "/", `[/\\]`)
	}
	return effectivePattern, true, ignoreCase
}

// isAbsoluteGlobPattern reports whether a pattern is absolute on this platform:
// a leading "/" on POSIX, a drive letter or UNC prefix on Windows.
func isAbsoluteGlobPattern(pattern string) bool {
	if strings.HasPrefix(pattern, "/") {
		return true
	}
	if len(pattern) >= 3 && pattern[1] == ':' && pattern[2] == '/' {
		return true
	}
	return len(pattern) >= 2 && pattern[0] == '/' && pattern[1] == '/'
}

// escapeGlobMeta escapes the characters a glob treats specially, so a search
// path is matched literally: upstream's `/[\\*?[\]{}]/g` replacement.
func escapeGlobMeta(path string) string {
	var escaped strings.Builder
	escaped.Grow(len(path))
	for _, char := range path {
		switch char {
		case '\\', '*', '?', '[', ']', '{', '}':
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(char)
	}
	return escaped.String()
}

// CreateFindTool builds the find tool.
func CreateFindTool(cwd string) agent.AgentTool {
	return agent.AgentTool{
		Name:        "find",
		Description: FindToolDescription,
		Parameters:  findSchemaJSON,
		Label:       "find",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}
			var input struct {
				Pattern string   `json:"pattern"`
				Path    string   `json:"path"`
				Limit   *float64 `json:"limit"`
			}
			if len(params) > 0 {
				if err := jsonUnmarshalStrictTool(params, &input); err != nil {
					return agent.AgentToolResult{}, err
				}
			}
			searchPath := ResolveToCwd(orDefault(input.Path, "."), cwd)
			effectiveLimit := float64(findDefaultLimit)
			if input.Limit != nil {
				effectiveLimit = *input.Limit
			}
			// fd treats --max-results 0 as unlimited, so a limit below 1 means 1,
			// and the schema's Number is floored (pi v1.5.2 find.ts).
			effectiveLimit = math.Max(1, math.Floor(effectiveLimit))

			// The reference resolves fd before spawning and never stats the path:
			// fd's own error (or the unavailable-tool message) is what callers see.
			fdPath, err := exec.LookPath("fd")
			if err != nil {
				fdPath, err = exec.LookPath("fdfind")
			}
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("fd is not available and could not be downloaded")
			}
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}

			args := []string{"--glob", "--color=never", "--hidden"}
			// fd normally ignores .gitignore outside git repos, so keep
			// --no-require-git there; inside repos use fd's git-aware default
			// so parent .gitignore rules stop at nested repo boundaries.
			insideGitRepo := false
			for current := searchPath; ; {
				if fileExists(filepath.Join(current, ".git")) {
					insideGitRepo = true
					break
				}
				parent := filepath.Dir(current)
				if parent == current {
					break
				}
				current = parent
			}
			if !insideGitRepo {
				args = append(args, "--no-require-git")
			}
			// One result past the limit tells "exactly N matches" apart from
			// "more than N".
			args = append(args, "--max-results", formatToolNumber(effectiveLimit+1))

			// fd --glob matches basenames unless --full-path; a path pattern is
			// anchored at the search directory and matched case-insensitively when
			// the user's pattern is all lowercase (pi v1.6.0 find.ts).
			effectivePattern, fullPath, ignoreCase := findFullPathVariable(input.Pattern, searchPath, runtime.GOOS)
			if fullPath {
				args = append(args, "--full-path")
			}
			if ignoreCase {
				args = append(args, "--ignore-case")
			}
			args = append(args, "--", effectivePattern, searchPath)

			cmd := exec.CommandContext(ctx, fdPath, args...)
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}
			output := strings.TrimRight(DecodeUTF8WithReplacement([]byte(stdout.String())), "\n")
			if runErr != nil {
				// A spawn failure is not an exit code (upstream find.ts's
				// child.on("error")).
				exitErr, exited := runErr.(*exec.ExitError)
				if !exited {
					return agent.AgentToolResult{}, fmt.Errorf("Failed to run fd: %s", runErr.Error())
				}
				errorMsg := strings.TrimSpace(stderr.String())
				if errorMsg == "" {
					errorMsg = fmt.Sprintf("fd exited with code %d", exitErr.ExitCode())
				}
				if output == "" {
					return agent.AgentToolResult{}, fmt.Errorf("%s", errorMsg)
				}
			}
			if output == "" {
				return agent.AgentToolResult{
					Content: []ai.Content{ai.TextContent{Text: "No files found matching pattern"}},
				}, nil
			}

			var relativized []string
			for rawLine := range strings.SplitSeq(output, "\n") {
				line := strings.TrimRight(rawLine, "\r")
				if line == "" {
					continue
				}
				relativized = append(relativized, RelativizeFindResultPath(line, searchPath))
			}
			resultLimitReached := float64(len(relativized)) > effectiveLimit
			// Compare as a number before converting: a limit above the int range
			// (fd accepts one) overflows the conversion and slices with a negative
			// bound. The reference slices with a JavaScript number, which clamps.
			if effectiveLimit < float64(len(relativized)) {
				relativized = relativized[:int(effectiveLimit)]
			}
			rawOutput2 := strings.Join(relativized, "\n")
			truncation := TruncateHead(rawOutput2, TruncationOptions{MaxLines: uncappedLines})
			resultOutput := truncation.Content
			details := FindToolDetails{}
			var notices []string
			if resultLimitReached {
				notices = append(notices, fmt.Sprintf("%s results limit reached. Use limit=%s for more, or refine pattern",
					formatToolNumber(effectiveLimit), formatToolNumber(effectiveLimit*2)))
				details.ResultLimitReached = &effectiveLimit
			}
			if truncation.Truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
				details.Truncation = &truncation
			}
			if len(notices) > 0 {
				resultOutput += "\n\n[" + strings.Join(notices, ". ") + "]"
			}
			var detailsJSON json.RawMessage
			if len(notices) > 0 {
				detailsJSON, _ = ai.MarshalJSON(details)
			}
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: resultOutput}},
				Details: detailsJSON,
			}, nil
		},
	}
}

// --- grep ---

const grepDefaultLimit = 100

// GrepToolDescription is upstream's grep description.
const GrepToolDescription = "Search file contents for a pattern. Returns matching lines with file paths and line numbers. Respects .gitignore. Output is truncated to 100 matches or 50KB (whichever is hit first). Long lines are truncated to 500 chars."

// GrepToolDetails carries grep truncation info.
type GrepToolDetails struct {
	Truncation        *TruncationResult `json:"truncation,omitempty"`
	MatchLimitReached *float64          `json:"matchLimitReached,omitempty"`
	LinesTruncated    *bool             `json:"linesTruncated,omitempty"`
}

var grepSchemaJSON = mustMarshalJSON(json.RawMessage(`{
	"type": "object",
	"required": ["pattern"],
	"properties": {
		"pattern": {"type": "string", "description": "Search pattern (regex or literal string)"},
		"path": {"type": "string", "description": "Directory or file to search (default: current directory)"},
		"glob": {"type": "string", "description": "Filter files by glob pattern relative to path, e.g. 'src/**/*.ts'. Without '/', e.g. '*.ts', it matches file names at any depth"},
		"ignoreCase": {"type": "boolean", "description": "Case-insensitive search (default: false)"},
		"literal": {"type": "boolean", "description": "Treat pattern as literal string instead of regex (default: false)"},
		"context": {"type": "number", "description": "Number of lines to show before and after each match (default: 0)"},
		"limit": {"type": "number", "description": "Maximum number of matches to return (default: 100)"}
	}
}`))

// rgMatchEvent is one ripgrep --json event.
type rgMatchEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text  string `json:"text"`
			Bytes string `json:"bytes"`
		} `json:"path"`
		LineNumber *int64 `json:"line_number"`
		Lines      struct {
			Text  *string `json:"text"`
			Bytes string  `json:"bytes"`
		} `json:"lines"`
	} `json:"data"`
}

// stripGlobDotSlash drops a leading "./" from a glob, keeping a leading "!" that
// negates it (pi v1.5.2 grep.ts: rg never matches a leading "./").
func stripGlobDotSlash(glob string) string {
	prefix := ""
	if strings.HasPrefix(glob, "!") {
		prefix, glob = "!", glob[1:]
	}
	for strings.HasPrefix(glob, "./") {
		glob = glob[2:]
	}
	return prefix + glob
}

// trimOneLineTerminator removes at most one trailing line terminator, the
// reference's `lineText.replace(/\r?\n?$/, "")`: a lone CR inside a line is
// content, as in the read tool.
func trimOneLineTerminator(text string) string {
	if strings.HasSuffix(text, "\r\n") {
		return text[:len(text)-2]
	}
	if strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r") {
		return text[:len(text)-1]
	}
	return text
}

// decodeRgText decodes a ripgrep JSON text field: non-UTF-8 data arrives
// base64-encoded in `bytes` (pi v1.5.2 grep.ts decodeRgText).
func decodeRgText(text *string, encoded string) string {
	if text != nil {
		return *text
	}
	if encoded == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return DecodeUTF8WithReplacement(decoded)
}

// CreateGrepTool builds the grep tool.
func CreateGrepTool(cwd string) agent.AgentTool {
	return agent.AgentTool{
		Name:        "grep",
		Description: GrepToolDescription,
		Parameters:  grepSchemaJSON,
		Label:       "grep",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}
			var input struct {
				Pattern    string   `json:"pattern"`
				Path       string   `json:"path"`
				Glob       string   `json:"glob"`
				IgnoreCase *bool    `json:"ignoreCase"`
				Literal    *bool    `json:"literal"`
				Context    *float64 `json:"context"`
				Limit      *float64 `json:"limit"`
			}
			if len(params) > 0 {
				if err := jsonUnmarshalStrictTool(params, &input); err != nil {
					return agent.AgentToolResult{}, err
				}
			}

			rgPath, err := exec.LookPath("rg")
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("ripgrep (rg) is not available and could not be downloaded")
			}
			searchPath := ResolveToCwd(orDefault(input.Path, "."), cwd)
			info, statErr := os.Stat(searchPath)
			if statErr != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Path not found: %s", searchPath)
			}
			isDirectory := info.IsDir()

			// pi v1.5.2 floors both numbers: the context lines and the match
			// limit are clamped so String() never yields exponent notation.
			contextValue := int64(0)
			if input.Context != nil && *input.Context > 0 {
				contextValue = int64(math.Floor(*input.Context))
			}
			// A JavaScript number, not an integer: upstream compares matchCount
			// against it, and a limit past the int64 range must simply never fire
			// instead of overflowing to a negative one.
			effectiveLimit := float64(grepDefaultLimit)
			if input.Limit != nil {
				effectiveLimit = math.Max(1, math.Floor(*input.Limit))
			}
			// rg runs inside the search path for a directory search, so it reports
			// paths relative to it; resolve before relativizing (pi v1.5.2 grep.ts).
			formatPath := func(filePath string) string {
				if isDirectory {
					resolved := filePath
					if !filepath.IsAbs(resolved) {
						resolved = filepath.Join(searchPath, resolved)
					}
					if rel, relErr := filepath.Rel(searchPath, resolved); relErr == nil && rel != "" && !strings.HasPrefix(rel, "..") {
						return filepath.ToSlash(rel)
					}
				}
				return filepath.Base(filePath)
			}

			// --no-config: the tool contract (literal, glob, limit, ...) must not
			// depend on ambient ripgrep configuration such as RIPGREP_CONFIG_PATH;
			// users customize through bash or extensions instead (upstream v1.4.0
			// 8c4e87389, #11203).
			args := []string{"--no-config", "--json", "--line-number", "--color=never", "--hidden"}
			if input.IgnoreCase != nil && *input.IgnoreCase {
				args = append(args, "--ignore-case")
			}
			if input.Literal != nil && *input.Literal {
				args = append(args, "--fixed-strings")
			}
			if input.Glob != "" {
				// rg never matches a leading "./" ("./src/*.ts" finds nothing),
				// and "!" negates the glob (pi v1.5.2 grep.ts).
				args = append(args, "--glob", stripGlobDotSlash(input.Glob))
			}
			// rg emits the surrounding lines as context events, so no matched file
			// is ever read here (pi v1.5.2 grep.ts, #9276).
			if contextValue > 0 {
				args = append(args, "--context", formatToolNumber(float64(contextValue)))
			}
			// rg roots --glob at its own cwd, so a directory search runs inside the
			// search path and searches "." (pi v1.5.2 grep.ts, #11807).
			target := searchPath
			if isDirectory {
				target = "."
			}
			args = append(args, "--", input.Pattern, target)

			cmd := exec.CommandContext(ctx, rgPath, args...)
			if isDirectory {
				cmd.Dir = searchPath
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Failed to run ripgrep: %s", err.Error())
			}
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Failed to run ripgrep: %s", err.Error())
			}

			matchCount := 0
			matchLimitReached := false
			linesTruncated := false
			killedDueToLimit := false
			var outputLines []string
			// Byte accounting covers the whole output, including lines past the
			// cap that are counted but not stored.
			outputBytes := 0
			outputLineCount := 0
			lastFile := ""
			// Set at the last allowed match. rg still sends that match's trailing
			// context, kept up to trailingContextEnd, and the limit notice waits
			// for proof of one more match.
			limitHit := false
			trailingContextEnd := int64(0)
			done := false
			stopForLimit := func() {
				if killedDueToLimit {
					return
				}
				killedDueToLimit = true
				_ = cmd.Process.Kill()
			}
			pushLine := func(filePath string, lineNumber int64, lineText string, isMatch bool) {
				lastFile = filePath
				relativePath := formatPath(filePath)
				truncated := TruncateLine(trimOneLineTerminator(lineText), GrepMaxLineLength)
				if truncated.WasTruncated {
					linesTruncated = true
				}
				number := formatToolNumber(float64(lineNumber))
				var line string
				if isMatch {
					line = fmt.Sprintf("%s:%s: %s", relativePath, number, truncated.Text)
				} else {
					line = fmt.Sprintf("%s-%s- %s", relativePath, number, truncated.Text)
				}
				// Byte truncation below drops everything past the cap, so keep
				// counting but stop storing.
				if outputBytes-1 <= DefaultMaxBytes {
					outputLines = append(outputLines, line)
				}
				outputBytes += len(line) + 1
				outputLineCount++
			}

			reader := bufio.NewReader(stdout)
			var readErr error
			for !done {
				line, lineErr := reader.ReadBytes('\n')
				if len(line) > 0 {
					trimmed := strings.TrimSpace(string(line))
					if trimmed != "" {
						var event rgMatchEvent
						if jsonUnmarshalStrictTool(json.RawMessage(trimmed), &event) == nil {
							isMatch := event.Type == "match"
							if isMatch || event.Type == "context" {
								filePath := decodeRgText(&event.Data.Path.Text, event.Data.Path.Bytes)
								lineNumber := event.Data.LineNumber
								if filePath != "" && lineNumber != nil {
									lineText := decodeRgText(event.Data.Lines.Text, event.Data.Lines.Bytes)
									if !limitHit {
										pushLine(filePath, *lineNumber, lineText, isMatch)
										if isMatch {
											matchCount++
											if float64(matchCount) >= effectiveLimit {
												limitHit = true
												trailingContextEnd = *lineNumber + contextValue
											}
										}
									} else {
										inWindow := filePath == lastFile && *lineNumber <= trailingContextEnd
										// A match inside the last match's trailing context is
										// shown as context.
										if inWindow {
											pushLine(filePath, *lineNumber, lineText, false)
										}
										// Only another match proves the limit cut results off.
										if isMatch {
											matchLimitReached = true
										}
										if matchLimitReached && (!inWindow || *lineNumber >= trailingContextEnd) {
											done = true
											stopForLimit()
										}
									}
								}
							}
						}
					}
				}
				if lineErr == io.EOF {
					break
				}
				if lineErr != nil {
					readErr = lineErr
					stopForLimit()
					break
				}
			}
			_ = stdout.Close()
			runErr := cmd.Wait()
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}
			if readErr != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Failed to read ripgrep output: %s", readErr.Error())
			}
			// Upstream rejects on any exit code other than 0/1 unless it killed
			// ripgrep itself to enforce the match limit.
			if runErr != nil && !killedDueToLimit {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					code := exitErr.ExitCode()
					if code != 0 && code != 1 {
						errorMsg := strings.TrimSpace(stderr.String())
						if errorMsg == "" {
							errorMsg = fmt.Sprintf("ripgrep exited with code %d", code)
						}
						return agent.AgentToolResult{}, fmt.Errorf("%s", errorMsg)
					}
				} else {
					return agent.AgentToolResult{}, fmt.Errorf("Failed to run ripgrep: %s", runErr.Error())
				}
			}
			if matchCount == 0 {
				return agent.AgentToolResult{
					Content: []ai.Content{ai.TextContent{Text: "No matches found"}},
				}, nil
			}

			rawOutput := strings.Join(outputLines, "\n")
			truncation := TruncateHead(rawOutput, TruncationOptions{MaxLines: uncappedLines})
			// Only the lines up to the byte cap are stored, so the totals come from
			// the streaming counts: upstream grep.ts sets totalLines/totalBytes from
			// every line ripgrep sent, stored or not, and the detail is persisted.
			truncation.TotalLines = outputLineCount
			truncation.TotalBytes = outputBytes - 1
			output := truncation.Content
			details := GrepToolDetails{}
			var notices []string
			if matchLimitReached {
				notices = append(notices, fmt.Sprintf("%s matches limit reached. Use limit=%s for more, or refine pattern",
					formatToolNumber(effectiveLimit), formatToolNumber(effectiveLimit*2)))
				details.MatchLimitReached = &effectiveLimit
			}
			if truncation.Truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
				details.Truncation = &truncation
			}
			if linesTruncated {
				notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", GrepMaxLineLength))
				details.LinesTruncated = new(true)
			}
			if len(notices) > 0 {
				output += "\n\n[" + strings.Join(notices, ". ") + "]"
			}
			var detailsJSON json.RawMessage
			if len(notices) > 0 {
				detailsJSON, _ = ai.MarshalJSON(details)
			}
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: output}},
				Details: detailsJSON,
			}, nil
		},
	}
}
