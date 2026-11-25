package interactive

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
	"github.com/dat267/pier/tui"
)

// Port of the remaining command handlers of
// src/modes/interactive/interactive-mode.ts (getPathCommandArgument,
// handleExport/Import/Copy/Name/Session/Hotkeys/Clear/Debug/Compact/Reload/
// Changelog commands, the easter eggs and stop). The session-share and
// bug-report flows (D316) are not part of this port.
//
// Divergences: the runtime/clipboard/report collaborators are injected (D90);
// the extension seams stay out of scope (D41).

// CommandSession is the session surface the command handlers need.
type CommandSession interface {
	GetSessionStats() *coding.SessionStats
	GetLastAssistantText() string
	SetSessionName(name string)
	ExportToJsonl(outputPath string) (string, error)
	CompactSession(ctx context.Context, customInstructions string) error
	GetCacheWarmingStatus() *coding.CacheWarmingStatus
	Model() *ai.Model
	Messages() []ai.Message
	ModelRuntime() *coding.ModelRuntime
	IsStreaming() bool
	IsCompacting() bool
}

// CommandWiring handles the slash commands.
type CommandWiring struct {
	Chat        *tui.Container
	UI          tui.TUI
	Settings    *coding.SettingsManager
	Session     CommandSession
	SessionInfo *coding.SessionManager
	AppName     string
	Platform    string

	// ShowStatus/ShowError/ShowWarning report messages.
	ShowStatus  func(message string)
	ShowError   func(message string)
	ShowWarning func(message string)
	// ReloadGoal replays the goal durable state after a reload (the extension's
	// session_start reason "reload" pauses a live goal).
	ReloadGoal func()
	// LoopScheduler owns this session's scheduled prompts, and LoopMaxAge is a
	// recurring job's lifetime.
	LoopScheduler *coding.LoopScheduler
	LoopMaxAge    time.Duration
	// OnLoopPrompt submits a loop's prompt through the ordinary input path.
	OnLoopPrompt func(prompt string)
	// LoopFeatureEnabled reports whether /loop is enabled, so a disabled feature
	// can say so instead of doing nothing.
	LoopFeatureEnabled func() bool
	// RunGoalCommand hands `/goal` arguments to the session's goal controller
	// and reports false when there is none (the goal feature is off, D496).
	RunGoalCommand func(args string) bool
	// RequestRender requests a render.
	RequestRender func()
	// ClearStatusIndicator clears the active indicator.
	ClearStatusIndicator func()

	// Exporters resolves the current session's exporters. The command calls it
	// on the owner loop, so the queued worker exports the session the command
	// was issued on instead of reading the wiring after a switch.
	Exporters func() SessionExporters
	// ExportQueue runs the export off the owner loop; ExportQueue or PostToUI
	// nil exports synchronously (tests and embedders).
	ExportQueue *offloop.Queue
	// PostToUI delivers the export's result back to the owner loop.
	PostToUI func(func())
	// HandleFatalRuntimeError reports a fatal runtime error and shuts down
	// (upstream handleFatalRuntimeError).
	HandleFatalRuntimeError func(ctx context.Context, prefix string, err error)
	// NewSession starts a new session.
	NewSession func(ctx context.Context) (bool, error)
	// ImportFromJSONL imports a session ("" cwd = the session's cwd). It reports
	// through onDone: the file is copied and read off the owner loop.
	ImportFromJSONL func(ctx context.Context, inputPath string, cwdOverride string, onDone func(bool, error))
	// ShowExtensionConfirm asks a yes/no question; the answer arrives through the
	// callback on the UI loop (upstream's extension-UI confirm, awaited there).
	ShowExtensionConfirm func(ctx context.Context, title string, message string, onAnswer func(confirmed bool))
	// PromptForMissingCwd resolves a missing-cwd error.
	PromptForMissingCwd func(ctx context.Context, issue coding.SessionCwdIssue, onAnswer func(choice coding.MissingSessionCwdChoice))
	// ForkIntoSession forks a session whose stored cwd is gone into cwd and
	// switches to the fork (upstream switchToForkedSession).
	ForkIntoSession func(ctx context.Context, sourcePath string, cwd string, onDone func(*ForkedSessionResult, error))
	// CopyToClipboard copies text, returning (ok, message).
	CopyToClipboard func(text string) (bool, string)
	// CopyActiveSelection copies the alt-screen selection.
	CopyActiveSelection func() bool
	// WriteDebugLog writes the debug log.
	WriteDebugLog func(content string) error
	// MarkdownTheme is used for the changelog/hotkeys rendering.
	MarkdownTheme func() tui.MarkdownTheme

	// Reload hooks (upstream handleReloadCommand; extension mechanics are
	// out of scope, D41).
	EditorContainer *tui.Container
	Editor          tui.Component
	// RunDetached runs the blocking reload work off the UI loop.
	RunDetached func(fn func()) bool
	// DefaultSyncNotice reports the defaultsync sync outcome of the last
	// reload (D151): the notice text, and whether it is a warning. A reload
	// that moved the model must say so rather than change it silently.
	DefaultSyncNotice func() (string, bool)
	// ReloadNow re-reads settings-dependent state on the UI loop (settings
	// file, session queue modes, keybindings, implicit project trust). It
	// returns the models.json error ("" = none) and whether implicit project
	// trust was saved. Everything it writes is owner-loop state, so the loop
	// runs it; only the MCP rebuild below is detached.
	ReloadNow func() (modelsJSONError string, savedTrust bool, err error)
	// ReloadMCP resolves the MCP half of a reload on the loop. Build re-reads
	// mcp.json and starts a new manager off the loop and writes no app state;
	// Apply installs it on the loop (upstream reload(): session_shutdown
	// invalidates the extension's connections and session_start reconnects
	// them). A nil plan means no MCP wiring; a nil built manager means none is
	// configured, and its connection failures are the second return.
	ReloadMCP func() ReloadMCPPlan
	// ApplyReloadedSettings re-applies settings-dependent UI state on the
	// loop (runtime settings, chat rebuild, themes, autocomplete).
	ApplyReloadedSettings func()
}

func (w *CommandWiring) showStatus(message string) {
	if w.ShowStatus != nil {
		w.ShowStatus(message)
	}
}

func (w *CommandWiring) showError(message string) {
	if w.ShowError != nil {
		w.ShowError(message)
	}
}

func (w *CommandWiring) showWarning(message string) {
	if w.ShowWarning != nil {
		w.ShowWarning(message)
	}
}

func (w *CommandWiring) requestRender() {
	if w.RequestRender != nil {
		w.RequestRender()
	} else if w.UI != nil {
		w.UI.RequestRender(false)
	}
}

func (w *CommandWiring) markdownTheme() tui.MarkdownTheme {
	if w.MarkdownTheme != nil {
		return w.MarkdownTheme()
	}
	return GetMarkdownTheme()
}

// GetPathCommandArgument extracts the path argument of /export or /import.
func GetPathCommandArgument(text string, command string) (string, bool) {
	if text == command {
		return "", false
	}
	if !strings.HasPrefix(text, command+" ") {
		return "", false
	}
	args := ai.JSTrimLeft(text[len(command)+1:])
	if args == "" {
		return "", false
	}
	first := args[0]
	if first == '"' || first == '\'' {
		closing := strings.IndexByte(args[1:], first)
		if closing < 0 {
			return "", false
		}
		return args[1 : 1+closing], true
	}
	if index := strings.IndexFunc(args, ai.IsJSSpace); index >= 0 {
		return args[:index], true
	}
	return args, true
}

// SessionExporters are one session's exporters, bound to that session and
// resolved on the owner loop.
type SessionExporters struct {
	JSONL func(outputPath string) (string, error)
	HTML  func(outputPath string) (string, error)
}

// resolveExporters binds the exporters to the session the command runs on. A
// CommandSession export falls back to JSONL when the wiring supplies no
// exporters (tests and embedders).
func (w *CommandWiring) resolveExporters() SessionExporters {
	var exporters SessionExporters
	if w.Exporters != nil {
		exporters = w.Exporters()
	}
	if exporters.JSONL == nil && w.Session != nil {
		exporters.JSONL = w.Session.ExportToJsonl
	}
	return exporters
}

// HandleExportCommand exports the session as JSONL or HTML.
//
// Serializing a long session re-chains and re-marshals every entry, which froze
// the UI for hundreds of milliseconds. The work runs on ExportQueue when one is
// wired and reports back through PostToUI, so the loop keeps painting; the
// synchronous path is what tests and embedders use.
func (w *CommandWiring) HandleExportCommand(ctx context.Context, text string) {
	outputPath, hasPath := GetPathCommandArgument(text, "/export")
	exporters := w.resolveExporters()
	run := func() (string, error) {
		if hasPath && strings.HasSuffix(outputPath, ".jsonl") {
			return exporters.JSONL(outputPath)
		}
		if exporters.HTML == nil {
			return "", errExportHTMLUnavailable
		}
		return exporters.HTML(outputPath)
	}
	report := func(filePath string, err error) {
		if err != nil {
			w.showError("Failed to export session: " + err.Error())
			return
		}
		w.showStatus("Session exported to: " + filePath)
	}
	if w.ExportQueue == nil || w.PostToUI == nil {
		filePath, err := run()
		report(filePath, err)
		return
	}
	w.showStatus("Exporting session…")
	if !w.ExportQueue.GoLatestContext("session-export", func(ctx context.Context) {
		filePath, err := run()
		if ctx.Err() != nil {
			return
		}
		w.PostToUI(func() {
			if ctx.Err() != nil {
				return
			}
			report(filePath, err)
		})
	}) {
		// The queue is stopped (shutdown), so the export will never run and the
		// notice would stay on screen.
		w.showStatus("")
	}
}

// errExportHTMLUnavailable reports a missing HTML exporter.
var errExportHTMLUnavailable = errors.New("HTML export is not available")

// HandleImportCommand imports a session from JSONL.
func (w *CommandWiring) HandleImportCommand(ctx context.Context, text string) {
	inputPath, ok := GetPathCommandArgument(text, "/import")
	if !ok || inputPath == "" {
		w.showError("Usage: /import <path.jsonl>")
		return
	}

	// importWith runs the import and, when the session's stored cwd is gone,
	// offers to continue in the fallback before trying again — upstream's
	// await, then await again, as a callback chain.
	var importWith func(cwdOverride string)
	importWith = func(cwdOverride string) {
		if w.ImportFromJSONL == nil {
			w.showStatus("Import cancelled")
			return
		}
		if w.ClearStatusIndicator != nil {
			w.ClearStatusIndicator()
		}
		w.ImportFromJSONL(ctx, inputPath, cwdOverride, func(cancelled bool, err error) {
			if err == nil {
				if cancelled {
					w.showStatus("Import cancelled")
					return
				}
				w.showStatus("Session imported from: " + inputPath)
				return
			}
			if notFound, ok := errors.AsType[*SessionImportFileNotFoundError](err); ok {
				w.showError("Failed to import session: " + notFound.Error())
				return
			}
			var cwdErr *coding.MissingSessionCwdError
			if w.PromptForMissingCwd != nil && errors.As(err, &cwdErr) {
				w.PromptForMissingCwd(ctx, cwdErr.Issue, func(choice coding.MissingSessionCwdChoice) {
					switch choice {
					case coding.MissingSessionCwdContinue:
						importWith(cwdErr.Issue.FallbackCwd)
					case coding.MissingSessionCwdFork:
						w.forkImportIntoSession(ctx, inputPath, cwdErr.Issue.FallbackCwd)
					default:
						w.showStatus("Import cancelled")
					}
				})
				return
			}
			if w.HandleFatalRuntimeError != nil {
				w.HandleFatalRuntimeError(ctx, "Failed to import session", err)
				return
			}
			w.showError("Failed to import session: " + err.Error())
		})
	}

	if w.ShowExtensionConfirm == nil {
		importWith("")
		return
	}
	w.ShowExtensionConfirm(ctx, "Import session", "Replace current session with "+inputPath+"?", func(confirmed bool) {
		if !confirmed {
			w.showStatus("Import cancelled")
			return
		}
		importWith("")
	})
}

// forkImportIntoSession forks the imported session into cwd and switches to the
// fork (upstream handleImportCommand's fork case): a vetoed switch reports the
// import as cancelled.
func (w *CommandWiring) forkImportIntoSession(ctx context.Context, sourcePath string, cwd string) {
	if w.ForkIntoSession == nil {
		w.showStatus("Import cancelled")
		return
	}
	w.ForkIntoSession(ctx, sourcePath, cwd, func(forked *ForkedSessionResult, err error) {
		if err != nil {
			if w.HandleFatalRuntimeError != nil {
				w.HandleFatalRuntimeError(ctx, "Failed to import session", err)
				return
			}
			w.showError("Failed to import session: " + err.Error())
			return
		}
		if forked == nil || forked.Cancelled {
			w.showStatus("Import cancelled")
			return
		}
		w.showStatus(forked.Status())
	})
}

// ReloadMCPPlan is the MCP half of a /reload, resolved on the owner loop so the
// detached build sees the loop's snapshot instead of reading its state.
type ReloadMCPPlan struct {
	// Build re-reads mcp.json and starts a new manager. It must not write app
	// or session state; the caller runs it off the loop.
	Build func(ctx context.Context) (*coding.McpManager, []string)
	// Apply installs the built manager on the loop. It drops a manager built
	// for a cwd the session has left.
	Apply func(manager *coding.McpManager)
}

// reloadedItems names what a reload re-reads, for the /reload notice. The MCP
// manager re-reads mcp.json and reconnects (ReloadMCP), so the text now names
// what the port actually re-reads: upstream's text leads with "extensions", and
// the MCP half runs through the CLI-owned manager instead of an extension.
const reloadedItems = "keybindings, skills, prompts, themes, context files, and MCP servers"

// HandleReloadCommand runs /reload (upstream handleReloadCommand): guard
// streaming/compacting, swap the editor for a reload box, run the reload
// work detached so the box paints, then re-apply settings-dependent state
// and restore the editor. Extension mechanics are out of scope (D41).
func (w *CommandWiring) HandleReloadCommand() {
	if w.Session != nil && w.Session.IsStreaming() {
		w.showWarning("Wait for the current response to finish before reloading.")
		return
	}
	if w.Session != nil && w.Session.IsCompacting() {
		w.showWarning("Wait for compaction to finish before reloading.")
		return
	}
	if w.EditorContainer == nil || w.Editor == nil || w.ReloadNow == nil {
		return
	}

	theme := ActiveTheme()
	reloadBox := &tui.Container{}
	reloadBox.AddChild(NewDynamicBorder(nil))
	reloadBox.AddChild(tui.NewSpacer(1))
	reloadBox.AddChild(tui.NewText(theme.Fg("muted", "Reloading "+reloadedItems+"..."), 1, 0, nil))
	reloadBox.AddChild(tui.NewSpacer(1))
	reloadBox.AddChild(NewDynamicBorder(nil))

	// Upstream swaps the editor, focuses the box and awaits a nextTick so the
	// box paints before the reload; the port runs the reload work detached so
	// the loop keeps painting while it runs.
	w.EditorContainer.Clear()
	w.EditorContainer.AddChild(reloadBox)
	if w.UI != nil {
		w.UI.SetFocus(reloadBox)
		w.UI.RequestRender(true)
	}

	restore := func() {
		w.EditorContainer.Clear()
		w.EditorContainer.AddChild(w.Editor)
		if w.UI != nil {
			w.UI.SetFocus(w.Editor)
			w.UI.RequestRender(false)
		}
	}
	finish := func(modelsJSONError string, savedTrust bool, err error, connectionErrors []string) {
		if err != nil {
			restore()
			w.showError("Reload failed: " + err.Error())
			return
		}
		if w.ApplyReloadedSettings != nil {
			w.ApplyReloadedSettings()
		}
		if modelsJSONError != "" {
			w.showError("models.json error: " + modelsJSONError)
		}
		for _, message := range connectionErrors {
			w.showWarning(message)
		}
		if savedTrust {
			w.showStatus("Reloaded " + reloadedItems + "; saved project trust")
		} else {
			w.showStatus("Reloaded " + reloadedItems)
		}
		if w.DefaultSyncNotice != nil {
			if message, warning := w.DefaultSyncNotice(); message != "" {
				if warning {
					w.showWarning(message)
				} else {
					w.showStatus(message)
				}
			}
		}
		restore()
		if w.ReloadGoal != nil {
			w.ReloadGoal()
		}
	}

	// Settings, session resources, keybindings and trust are owner-loop state,
	// so the loop reloads them first: upstream's order, and the MCP config read
	// then sees the reloaded project trust.
	modelsJSONError, savedTrust, err := w.ReloadNow()
	if err != nil {
		restore()
		w.showError("Reload failed: " + err.Error())
		return
	}
	var plan ReloadMCPPlan
	if w.ReloadMCP != nil {
		plan = w.ReloadMCP()
	}
	deliver := func(manager *coding.McpManager, built bool, connectionErrors []string) {
		if !built {
			connectionErrors = nil
		} else if plan.Apply != nil {
			plan.Apply(manager)
		}
		finish(modelsJSONError, savedTrust, nil, connectionErrors)
	}
	if plan.Build == nil {
		deliver(nil, false, nil)
		return
	}
	if w.RunDetached != nil {
		accepted := w.RunDetached(func() {
			// Only the rebuild runs detached; it writes no app state, so the
			// loop installs the result when it lands. Its connection failures
			// are reported below, like the boot's startup warnings.
			manager, connectionErrors := plan.Build(context.Background())
			built := manager != nil
			if w.UI != nil {
				w.UI.Post(func() { deliver(manager, built, connectionErrors) })
				return
			}
			deliver(manager, built, connectionErrors)
		})
		if !accepted {
			restore()
			w.showWarning("MCP servers were not reloaded: background work is busy.")
		}
		return
	}
	manager, connectionErrors := plan.Build(context.Background())
	deliver(manager, manager != nil, connectionErrors)
}

// HandleCopyCommand copies the selection or the last assistant message.
func (w *CommandWiring) HandleCopyCommand(flashConfirmation bool, preferSelection bool) {
	if preferSelection {
		if altScreen, ok := tuiConcrete(w.UI).(*tui.AltScreen); ok && !altScreen.GetCopyOnSelect() && altScreen.HasActiveSelection() {
			if w.CopyActiveSelection != nil {
				w.CopyActiveSelection()
				return
			}
			altScreen.CopyActiveSelectionToClipboard()
			return
		}
	}
	text := w.Session.GetLastAssistantText()
	if text == "" {
		w.showError("No agent messages to copy yet.")
		return
	}
	if w.CopyToClipboard == nil {
		w.showError("Clipboard is not available")
		return
	}
	ok, message := w.CopyToClipboard(text)
	if !ok {
		w.showError(message)
		return
	}
	if flashConfirmation {
		if altScreen, isAlt := tuiConcrete(w.UI).(*tui.AltScreen); isAlt {
			altScreen.Flash("Copied!")
			return
		}
	}
	w.showStatus("Copied last agent message to clipboard")
}

// HandleNameCommand sets or prints the session name.
func (w *CommandWiring) HandleNameCommand(text string) {
	name := ai.JSTrim(strings.TrimPrefix(text, "/name"))
	if name == "" {
		// The name comes from the session file, so it is untrusted text for the
		// terminal (D342, D448).
		currentName := tui.SanitizeUntrustedText(w.SessionInfo.GetSessionName())
		if currentName != "" {
			theme := ActiveTheme()
			w.Chat.AddChild(tui.NewSpacer(1))
			w.Chat.AddChild(tui.NewText(theme.Fg("dim", "Session name: "+currentName), 1, 0, nil))
		} else {
			w.showWarning("Usage: /name <name>")
		}
		w.requestRender()
		return
	}

	w.Session.SetSessionName(name)
	sessionName := w.SessionInfo.GetSessionName()
	theme := ActiveTheme()
	if sessionName != name {
		w.showWarning("Session name was normalized from " + ai.QuoteJSON(name) + " to " + ai.QuoteJSON(sessionName))
	}
	w.Chat.AddChild(tui.NewSpacer(1))
	display := tui.SanitizeUntrustedText(sessionName)
	if display == "" {
		display = tui.SanitizeUntrustedText(name)
	}
	w.Chat.AddChild(tui.NewText(theme.Fg("dim", "Session name set: "+display), 1, 0, nil))
	w.requestRender()
}

// HandleSessionCommand renders the session info panel.
//
// The panel's numbers come from whole-session scans, and on a large session the
// cold scan is hundreds of milliseconds of JSON work (0.87s on a 19k-entry
// session). Upstream computes the panel inline, which here meant the UI loop
// could not render or route input until it finished. So the text is built off
// the loop and posted back to it: dispatch returns immediately and the panel
// lands on the next pump (D159). What it reads is session state behind mutexes
// (SettingsManager, SessionManager, AgentState, Projection), never the
// component tree.
func (w *CommandWiring) HandleSessionCommand(now int64) {
	if w.Chat == nil || w.UI == nil {
		// No loop to post to (headless wiring): compute inline, as upstream does.
		w.addSessionInfoPanel(w.SessionInfoPanel(now))
		return
	}
	// Resolve the session on the owner loop: the worker must describe the
	// session /session was issued on, and the loop rewrites the wiring on every
	// rebind, so reading it there mixed the old stats with the new name.
	session, sessionInfo := w.Session, w.SessionInfo
	go func() {
		text := w.sessionInfoPanel(session, sessionInfo, now)
		w.UI.Post(func() { w.addSessionInfoPanel(text) })
	}()
}

// addSessionInfoPanel appends a formatted panel to the chat. Runs on the UI loop.
func (w *CommandWiring) addSessionInfoPanel(text string) {
	if w.Chat == nil {
		return
	}
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(tui.NewText(text, 1, 0, nil))
	w.requestRender()
}

// SessionInfoPanel formats the session info panel. It only reads mutex-guarded
// session state, so it is safe to run off the UI loop.
func (w *CommandWiring) SessionInfoPanel(now int64) string {
	return w.sessionInfoPanel(w.Session, w.SessionInfo, now)
}

// sessionInfoPanel formats the panel from the session the caller resolved, so a
// worker never reads the live wiring.
func (w *CommandWiring) sessionInfoPanel(session CommandSession, sessionInfo *coding.SessionManager, now int64) string {
	theme := ActiveTheme()
	stats := session.GetSessionStats()
	sessionName := sessionInfo.GetSessionName()
	// These three walks read through the session's message memo; re-parsing the
	// session for a panel cost about 1.5s on a 19k-entry session and froze the UI.
	cacheWaste := sessionInfo.ComputeCacheWaste(session.ModelRuntime())
	usageBreakdown := sessionInfo.UsageCostBreakdown()

	var info strings.Builder
	info.WriteString(theme.Bold("Session Info"))
	info.WriteString("\n\n")
	if sessionName != "" {
		info.WriteString(theme.Fg("dim", "Name:"))
		info.WriteString(" ")
		info.WriteString(sessionName)
		info.WriteString("\n")
	}
	sessionFile := cmp.Or(stats.SessionFile, "In-memory")
	info.WriteString(theme.Fg("dim", "File:"))
	info.WriteString(" ")
	info.WriteString(sessionFile)
	info.WriteString("\n")
	info.WriteString(theme.Fg("dim", "ID:"))
	info.WriteString(" ")
	info.WriteString(stats.SessionID)
	info.WriteString("\n\n")
	info.WriteString(theme.Bold("Messages"))
	info.WriteString("\n")
	info.WriteString(theme.Fg("dim", "Total:"))
	info.WriteString(" ")
	info.WriteString(strconv.Itoa(stats.TotalMessages))
	info.WriteString("\n")
	info.WriteString(theme.Fg("dim", "User:"))
	info.WriteString(" ")
	info.WriteString(strconv.Itoa(stats.UserMessages))
	info.WriteString("\n")
	info.WriteString(theme.Fg("dim", "Assistant:"))
	info.WriteString(" ")
	info.WriteString(strconv.Itoa(stats.AssistantMessages))
	info.WriteString("\n")
	info.WriteString(theme.Fg("dim", "Tools:"))
	info.WriteString(" ")
	info.WriteString(strconv.Itoa(stats.ToolCalls))
	info.WriteString(" calls, ")
	info.WriteString(strconv.Itoa(stats.ToolResults))
	info.WriteString(" results\n\n")
	info.WriteString(theme.Bold("Tokens"))
	info.WriteString("\n")
	promptTokens := int64(ai.CalculateContextTokens(ai.Usage{
		Input: stats.Tokens.Input, CacheRead: stats.Tokens.CacheRead, CacheWrite: stats.Tokens.CacheWrite,
	}))
	info.WriteString(theme.Fg("dim", "Input:"))
	info.WriteString(" ")
	info.WriteString(formatThousands(promptTokens))
	info.WriteString("\n")
	if promptTokens > 0 && (stats.Tokens.CacheRead > 0 || stats.Tokens.CacheWrite > 0) {
		hitRate := theme.Fg("dim", "("+ai.JSNumberToFixed(float64(stats.Tokens.CacheRead)/float64(promptTokens)*100, 1)+"%)")
		info.WriteString("  ")
		info.WriteString(theme.Fg("dim", "Cached:"))
		info.WriteString(" ")
		info.WriteString(formatThousands(stats.Tokens.CacheRead))
		info.WriteString(" ")
		info.WriteString(hitRate)
		info.WriteString("\n")
		written := ""
		if stats.Tokens.CacheWrite > 0 {
			written = " " + theme.Fg("dim", "("+formatThousands(stats.Tokens.CacheWrite)+" written to cache)")
		}
		uncached := int64(ai.CalculateContextTokens(ai.Usage{Input: stats.Tokens.Input, CacheWrite: stats.Tokens.CacheWrite}))
		info.WriteString("  ")
		info.WriteString(theme.Fg("dim", "Uncached:"))
		info.WriteString(" ")
		info.WriteString(formatThousands(uncached))
		info.WriteString(written)
		info.WriteString("\n")
	}
	info.WriteString(theme.Fg("dim", "Output:"))
	info.WriteString(" ")
	info.WriteString(formatThousands(stats.Tokens.Output))
	info.WriteString("\n")
	info.WriteString(theme.Fg("dim", "Total:"))
	info.WriteString(" ")
	info.WriteString(formatThousands(stats.Tokens.Total))
	info.WriteString("\n")

	info.WriteString("\n")
	info.WriteString(theme.Bold("Cache Warming"))
	info.WriteString("\n")
	cacheWarmingMode := ""
	if w.Settings != nil {
		cacheWarmingMode = w.Settings.GetCacheWarmingMode()
	}
	info.WriteString(theme.Fg("dim", "Mode:"))
	info.WriteString(" ")
	info.WriteString(cacheWarmingMode)
	info.WriteString("\n")
	status := session.GetCacheWarmingStatus()
	statusText := "Inactive (cache warming unavailable)"
	if status != nil {
		statusText = coding.FormatCacheWarmingStatus(*status, now)
	}
	info.WriteString(theme.Fg("dim", "Status:"))
	info.WriteString(" ")
	info.WriteString(statusText)
	info.WriteString("\n")
	if status != nil && status.Decision != nil && status.Decision.EconomicsAvailable {
		info.WriteString(theme.Fg("dim", "Cache miss penalty:"))
		info.WriteString(" $")
		info.WriteString(ai.JSNumberToFixed(status.Decision.MissCost, 3))
		info.WriteString("\n")
		info.WriteString(theme.Fg("dim", "Refresh cost:"))
		info.WriteString(" $")
		info.WriteString(ai.JSNumberToFixed(status.Decision.WarmCost, 3))
		info.WriteString("\n")
	}

	if stats.Cost > 0 || cacheWaste.MissedTokens > 0 {
		info.WriteString("\n")
		info.WriteString(theme.Bold("Cost"))
		info.WriteString("\n")
		info.WriteString(theme.Fg("dim", "Total:"))
		info.WriteString(" $")
		info.WriteString(ai.JSNumberToFixed(stats.Cost, 3))
		// Upstream shows the per-model breakdown unless the only entry names the
		// selected model (a single entry repeats the total).
		selectedModelKey := "undefined/undefined"
		if model := session.Model(); model != nil {
			selectedModelKey = model.Provider + "/" + model.ID
		}
		if len(usageBreakdown) != 1 || usageBreakdown[0].Key != selectedModelKey {
			for _, entry := range usageBreakdown {
				info.WriteString("\n  ")
				info.WriteString(theme.Fg("dim", entry.Key+":"))
				info.WriteString(" $")
				info.WriteString(ai.JSNumberToFixed(entry.Cost, 3))
				info.WriteString(" ")
				info.WriteString(theme.Fg("dim", "("+FormatTokens(entry.Tokens)+" tokens)"))
			}
		}
		if cacheWaste.MissedTokens > 0 {
			missLabel := strconv.Itoa(cacheWaste.MissCount) + " misses"
			if cacheWaste.MissCount == 1 {
				missLabel = "1 miss"
			}
			detail := formatThousands(cacheWaste.MissedTokens) + " tokens, " + missLabel
			if cacheWaste.MissedCost >= 0.0001 {
				info.WriteString("\n")
				info.WriteString(theme.Fg("dim", "Cache Re-billed:"))
				info.WriteString(" $")
				info.WriteString(ai.JSNumberToFixed(cacheWaste.MissedCost, 3))
				info.WriteString(" ")
				info.WriteString(theme.Fg("dim", "("+detail+")"))
			} else {
				info.WriteString("\n")
				info.WriteString(theme.Fg("dim", "Cache Re-billed:"))
				info.WriteString(" ")
				info.WriteString(detail)
			}
		}
	}

	return info.String()
}

// HandleHotkeysCommand renders the keyboard-shortcut table.
func (w *CommandWiring) HandleHotkeysCommand() {
	theme := ActiveTheme()
	editorKey := KeyDisplayText
	row := func(key string, action string) string { return "| `" + key + "` | " + action + " |\n" }

	var hotkeys strings.Builder
	hotkeys.WriteString("\n**Navigation**\n| Key | Action |\n|-----|--------|\n")
	hotkeys.WriteString(row(editorKey("tui.editor.cursorUp")+"` / `"+editorKey("tui.editor.cursorDown")+"` / `"+
		editorKey("tui.editor.cursorLeft")+"` / `"+editorKey("tui.editor.cursorRight"), "Move cursor / browse history"))
	hotkeys.WriteString(row(editorKey("tui.editor.cursorWordLeft")+"` / `"+editorKey("tui.editor.cursorWordRight"), "Move by word"))
	hotkeys.WriteString(row(editorKey("tui.editor.cursorLineStart"), "Start of line"))
	hotkeys.WriteString(row(editorKey("tui.editor.cursorLineEnd"), "End of line"))
	hotkeys.WriteString(row(editorKey("tui.editor.jumpForward"), "Jump forward to character"))
	hotkeys.WriteString(row(editorKey("tui.editor.jumpBackward"), "Jump backward to character"))
	hotkeys.WriteString(row(editorKey("tui.editor.pageUp")+"` / `"+editorKey("tui.editor.pageDown"), "Scroll by page"))

	hotkeys.WriteString("\n**Editing**\n| Key | Action |\n|-----|--------|\n")
	hotkeys.WriteString(row(editorKey("tui.input.submit"), "Send message"))
	newLineAction := "New line"
	if w.Platform == "win32" {
		newLineAction += " (Ctrl+Enter on Windows Terminal)"
	}
	hotkeys.WriteString(row(editorKey("tui.input.newLine"), newLineAction))
	hotkeys.WriteString(row(editorKey("tui.editor.deleteWordBackward"), "Delete word backwards"))
	hotkeys.WriteString(row(editorKey("tui.editor.deleteWordForward"), "Delete word forwards"))
	hotkeys.WriteString(row(editorKey("tui.editor.deleteToLineStart"), "Delete to start of line"))
	hotkeys.WriteString(row(editorKey("tui.editor.deleteToLineEnd"), "Delete to end of line"))
	hotkeys.WriteString(row(editorKey("tui.editor.yank"), "Paste the most-recently-deleted text"))
	hotkeys.WriteString(row(editorKey("tui.editor.yankPop"), "Cycle through the deleted text after pasting"))
	hotkeys.WriteString(row(editorKey("tui.editor.undo"), "Undo"))

	hotkeys.WriteString("\n**Other**\n| Key | Action |\n|-----|--------|\n")
	hotkeys.WriteString(row(editorKey("tui.input.tab"), "Path completion / accept autocomplete"))
	hotkeys.WriteString(row(KeyDisplayText("app.interrupt"), "Cancel autocomplete / abort streaming"))
	hotkeys.WriteString(row(KeyDisplayText("app.clear"), "Clear editor (first) / exit (second)"))
	hotkeys.WriteString(row(KeyDisplayText("app.exit"), "Exit (when editor is empty)"))
	hotkeys.WriteString(row(KeyDisplayText("app.suspend"), "Suspend to background"))
	hotkeys.WriteString(row(KeyDisplayText("app.thinking.cycle"), "Cycle thinking level"))
	hotkeys.WriteString(row(KeyDisplayText("app.model.cycleForward")+"` / `"+KeyDisplayText("app.model.cycleBackward"), "Cycle models"))
	hotkeys.WriteString(row(KeyDisplayText("app.model.select"), "Open model selector"))
	hotkeys.WriteString(row(KeyDisplayText("app.tools.expand"), "Toggle tool output expansion"))
	hotkeys.WriteString(row(KeyDisplayText("app.thinking.toggle"), "Toggle thinking block visibility"))
	hotkeys.WriteString(row(KeyDisplayText("app.editor.external"), "Edit message in external editor"))
	hotkeys.WriteString(row(KeyDisplayText("app.message.copy"), "Copy selection or last assistant message"))
	hotkeys.WriteString(row(KeyDisplayText("app.message.followUp"), "Queue follow-up message"))
	hotkeys.WriteString(row(KeyDisplayText("app.message.dequeue"), "Restore queued messages"))
	hotkeys.WriteString(row(KeyDisplayText("app.clipboard.pasteImage"), "Paste files on macOS, images, or text from clipboard"))
	hotkeys.WriteString(row("/", "Slash commands"))
	hotkeys.WriteString(row("!", "Run bash command"))
	hotkeys.WriteString(row("!!", "Run bash command (excluded from context)"))

	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(NewDynamicBorder(nil))
	w.Chat.AddChild(tui.NewText(theme.Bold(theme.Fg("accent", "Keyboard Shortcuts")), 1, 0, nil))
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(tui.NewMarkdown(strings.TrimSpace(hotkeys.String()), 1, 1, w.markdownTheme(), nil, tui.MarkdownOptions{}))
	w.Chat.AddChild(NewDynamicBorder(nil))
	w.requestRender()
}

// HandleChangelogCommand renders the full changelog (upstream
// handleChangelogCommand).
// HandleGoalCommand runs `/goal`. A disabled feature has no controller, so it
// reports itself rather than doing nothing, the way `/loop` does.
func (w *CommandWiring) HandleGoalCommand(args string) {
	if w.RunGoalCommand == nil || !w.RunGoalCommand(args) {
		w.showError("The goal feature is off. Turn it on in /features.")
	}
}

// HandleLoopCommand runs `/loop [interval] <prompt>`: with an interval it arms a
// cron job on the session's scheduler, and without one it starts a self-paced
// loop by running the prompt now, leaving the agent to arm its own wakeups.
// `/loop list` and `/loop stop [id]` show and cancel the scheduled jobs; only
// those exact shapes are commands, so a prompt such as "list the files" runs.
func (w *CommandWiring) HandleLoopCommand(args string) {
	// A disabled feature reports itself rather than silently doing nothing: the
	// row that turned it off is in /features, which is one keystroke away.
	if w.LoopFeatureEnabled != nil && !w.LoopFeatureEnabled() {
		w.showError("The loop feature is off. Turn it on in /features.")
		return
	}
	if fields := strings.Fields(args); len(fields) == 1 && fields[0] == "list" {
		w.listLoops()
		return
	} else if len(fields) > 0 && len(fields) <= 2 && fields[0] == "stop" {
		w.stopLoops(fields[1:])
		return
	}
	interval, prompt := coding.ParseLoopInput(args)
	if prompt == "" {
		w.showError("Usage: /loop [interval] <prompt>")
		return
	}
	if interval == 0 {
		w.runLoopPrompt(coding.LoopSelfPacedPrompt(prompt), "Loop started. The agent paces the next run itself.")
		return
	}
	cron, used, ok := coding.LoopCronForInterval(interval)
	if !ok {
		w.showError("The interval must be at most 28 days.")
		return
	}
	scheduler := w.loopScheduler()
	if scheduler == nil {
		w.showError("Scheduling is not available in this session.")
		return
	}
	// A whole-day step is advanced by the scheduler: cron's day-of-month step
	// restarts on the 1st, which is not the even spacing /loop promises.
	add := func() (coding.LoopJob, error) { return scheduler.Add(cron, prompt, true, w.LoopMaxAge) }
	if days := int(used / (24 * time.Hour)); days >= 1 && used%(24*time.Hour) == 0 {
		add = func() (coding.LoopJob, error) { return scheduler.AddEveryDays(days, prompt, w.LoopMaxAge) }
	}
	job, err := add()
	if err != nil {
		w.showError(err.Error())
		return
	}
	note := ""
	if used != interval {
		// Name the rounding: a cron step only spaces runs evenly when it
		// divides its unit, so 45m would otherwise fire at :00 and :45.
		typed := interval.String() // 7m0s, 1h30m0s, 30s
		if strings.HasSuffix(typed, "m0s") {
			typed = strings.TrimSuffix(typed, "0s")
		}
		if strings.HasSuffix(typed, "h0m") {
			typed = strings.TrimSuffix(typed, "0m")
		}
		note = "; " + typed + " rounded to an interval that divides evenly"
	}
	w.showStatus("Loop " + job.ID + " scheduled (" + job.HumanSchedule + note + ").")
}

// listLoops shows every scheduled job: its id, schedule, next run and prompt.
func (w *CommandWiring) listLoops() {
	scheduler := w.loopScheduler()
	if scheduler == nil {
		w.showError("Scheduling is not available in this session.")
		return
	}
	jobs := scheduler.Jobs()
	if len(jobs) == 0 {
		w.showStatus("No loops scheduled.")
		return
	}
	lines := []string{"Scheduled loops (/loop stop <id> cancels one, /loop stop all of them):"}
	now := scheduler.Now()
	for _, job := range jobs {
		lines = append(lines, fmt.Sprintf("%s  %s, next %s  %s",
			job.ID, job.HumanSchedule, loopNextRunLabel(job.NextFire, now), loopPromptSummary(job.Prompt)))
	}
	w.showStatus(strings.Join(lines, "\n"))
}

// loopPromptSummary keeps a job on one row of /loop list: a multi-line prompt
// (one the agent scheduled) shows its first line and " …" for the rest, so its
// later lines cannot read as rows of their own.
func loopPromptSummary(prompt string) string {
	prompt = coding.LoopPromptWithoutNote(prompt)
	first, rest, multiline := strings.Cut(prompt, "\n")
	first = strings.TrimSpace(first)
	if multiline && strings.TrimSpace(rest) != "" {
		return first + " …"
	}
	return first
}

// loopNextRunLabel names a job's next run in the schedule's own 12-hour style,
// with "tomorrow" or the date when it is not today: a bare clock made a daily
// loop's midnight run read like a time already past.
func loopNextRunLabel(next time.Time, now time.Time) string {
	next, now = next.Local(), now.Local()
	clock := next.Format("3:04 PM")
	year, month, day := now.Date()
	switch {
	case next.Year() == year && next.Month() == month && next.Day() == day:
		return clock
	case next.Before(time.Date(year, month, day+2, 0, 0, 0, 0, time.Local)):
		return "tomorrow " + clock
	}
	return next.Format("Mon Jan 2") + " " + clock
}

// stopLoops cancels the job named by ids, or every job when ids is empty.
func (w *CommandWiring) stopLoops(ids []string) {
	scheduler := w.loopScheduler()
	if scheduler == nil {
		w.showError("Scheduling is not available in this session.")
		return
	}
	if len(ids) == 0 {
		count := len(scheduler.Jobs())
		scheduler.RemoveAll()
		w.showStatus(fmt.Sprintf("Stopped %d loop(s).", count))
		return
	}
	if !scheduler.Remove(ids[0]) {
		w.showError("No loop " + ids[0] + ". /loop list shows the scheduled ones.")
		return
	}
	w.showStatus("Loop " + ids[0] + " stopped.")
}

// loopScheduler returns the session's scheduler, if one is installed.
func (w *CommandWiring) loopScheduler() *coding.LoopScheduler { return w.LoopScheduler }

// runLoopPrompt submits the loop's first prompt through the ordinary input path,
// so a loop is indistinguishable from a typed message.
func (w *CommandWiring) runLoopPrompt(prompt string, status string) {
	if w.OnLoopPrompt != nil {
		w.OnLoopPrompt(prompt)
		w.showStatus(status)
	}
}

func (w *CommandWiring) HandleChangelogCommand() {
	theme := ActiveTheme()
	entries := coding.ParseChangelog(coding.GetChangelogPath())
	markdown := "No changelog entries found."
	if len(entries) > 0 {
		// parseChangelog returns newest first; upstream reverses this order
		// before rendering the full changelog.
		parts := make([]string, 0, len(entries))
		for _, entry := range slices.Backward(entries) {

			parts = append(parts, coding.NormalizeChangelogLinks(entry.Content, changelogEntryVersion(entry)))
		}
		markdown = strings.Join(parts, "\n\n")
	}

	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(NewDynamicBorder(nil))
	w.Chat.AddChild(tui.NewText(theme.Bold(theme.Fg("accent", "What's New")), 1, 0, nil))
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(tui.NewMarkdown(markdown, 1, 1, w.markdownTheme(), nil, tui.MarkdownOptions{}))
	w.Chat.AddChild(NewDynamicBorder(nil))
	w.requestRender()
}

// HandleClearCommand starts a new session.
func (w *CommandWiring) HandleClearCommand(ctx context.Context) {
	if w.ClearStatusIndicator != nil {
		w.ClearStatusIndicator()
	}
	if w.NewSession == nil {
		return
	}
	cancelled, err := w.NewSession(ctx)
	if err != nil {
		if w.HandleFatalRuntimeError != nil {
			w.HandleFatalRuntimeError(ctx, "Failed to create session", err)
			return
		}
		w.showError("Failed to create session: " + err.Error())
		return
	}
	if cancelled {
		return
	}
	theme := ActiveTheme()
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(tui.NewText(theme.Fg("accent", "✓ New session started"), 1, 1, nil))
	w.requestRender()
}

// HandleDebugCommand writes the debug log.
func (w *CommandWiring) HandleDebugCommand(now string) {
	if w.UI == nil || w.WriteDebugLog == nil {
		return
	}
	terminal := w.UI.GetTerminal()
	width, height := 80, 24
	if terminal != nil {
		width = terminal.Columns()
		height = terminal.Rows()
	}
	allLines := w.UI.Render(width)
	debugLogPath := coding.GetDebugLogPath()
	lines := []string{
		"Debug output at " + now,
		"Terminal: " + strconv.Itoa(width) + "x" + strconv.Itoa(height),
		"Total lines: " + strconv.Itoa(len(allLines)),
		"",
		"=== All rendered lines with visible widths ===",
	}
	for index, line := range allLines {
		lines = append(lines, "["+strconv.Itoa(index)+"] (w="+strconv.Itoa(tui.VisibleWidth(line))+") "+ai.QuoteJSON(line))
	}
	lines = append(lines, "", "=== Agent messages (JSONL) ===")
	// Upstream writes the agent's context messages (session.messages), not the
	// raw session entries.
	if w.Session != nil {
		for _, message := range w.Session.Messages() {
			encoded, _ := ai.MarshalJSON(message)
			lines = append(lines, string(encoded))
		}
	}
	lines = append(lines, "")
	if err := w.WriteDebugLog(strings.Join(lines, "\n")); err != nil {
		w.showError("Failed to write debug log: " + err.Error())
		return
	}
	theme := ActiveTheme()
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(tui.NewText(theme.Fg("accent", "✓ Debug log written")+"\n"+theme.Fg("muted", debugLogPath), 1, 1, nil))
	w.requestRender()
}

// HandleArminSaysHi shows the armin easter egg.
func (w *CommandWiring) HandleArminSaysHi(host tui.RenderRequester, seed int64) {
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(NewArminComponent(host, "", seed))
	w.requestRender()
}

// HandleDementedDelves shows the Earendil announcement.
func (w *CommandWiring) HandleDementedDelves() {
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(NewEarendilAnnouncementComponent())
	w.requestRender()
}

// HandleDaxnuts shows the daxnuts easter egg.
func (w *CommandWiring) HandleDaxnuts(host tui.RenderRequester) {
	w.Chat.AddChild(tui.NewSpacer(1))
	w.Chat.AddChild(NewDaxnutsComponent(host))
	w.requestRender()
}

// CheckDaxnutsEasterEgg shows daxnuts for the OpenCode Kimi K2.5 model.
func (w *CommandWiring) CheckDaxnutsEasterEgg(provider string, modelID string, host tui.RenderRequester) {
	if provider == "opencode" && strings.Contains(strings.ToLower(modelID), "kimi-k2.5") {
		w.HandleDaxnuts(host)
	}
}

// HandleCompactCommand compacts the session (upstream shape: clear the
// indicator, then compact).
func (w *CommandWiring) HandleCompactCommand(ctx context.Context, customInstructions string) {
	w.ClearCompactionStatus()
	w.CompactSession(ctx, customInstructions)
}

// ClearCompactionStatus clears the active status indicator. It touches UI
// state, so it must run on the UI loop.
func (w *CommandWiring) ClearCompactionStatus() {
	if w.ClearStatusIndicator != nil {
		w.ClearStatusIndicator()
	}
}

// CompactSession runs the compaction. Its only UI effects are the session
// events it emits, so it is safe to run off the loop (stage 3: a manual
// compaction no longer blocks input).
func (w *CommandWiring) CompactSession(ctx context.Context, customInstructions string) {
	// Errors are emitted as session events.
	_ = w.Session.CompactSession(ctx, customInstructions)
}

// Stop tears the mode down.
func (w *CommandWiring) Stop(fullscreenExitOutput string, disposeSelector func(), clearExtensionListeners func(), disposeFooter func(), disposeFooterData func(), unsubscribe func(), stopInteractiveTui func(string), unregisterSignals func()) {
	if disposeSelector != nil {
		disposeSelector()
	}
	if w.Settings != nil && w.Settings.GetShowTerminalProgress() && w.UI != nil {
		if terminal := w.UI.GetTerminal(); terminal != nil {
			terminal.SetProgress(false)
		}
	}
	if w.ClearStatusIndicator != nil {
		w.ClearStatusIndicator()
	}
	if clearExtensionListeners != nil {
		clearExtensionListeners()
	}
	if disposeFooter != nil {
		disposeFooter()
	}
	if disposeFooterData != nil {
		disposeFooterData()
	}
	if unsubscribe != nil {
		unsubscribe()
	}
	if stopInteractiveTui != nil {
		stopInteractiveTui(fullscreenExitOutput)
	}
	if unregisterSignals != nil {
		unregisterSignals()
	}
}

// WriteDebugLogFile writes the debug log to the standard path.
func WriteDebugLogFile(content string) error {
	debugLogPath := coding.GetDebugLogPath()
	if err := os.MkdirAll(filepath.Dir(debugLogPath), 0o777); err != nil {
		return err
	}
	return coding.WritePrivateFile(debugLogPath, []byte(content))
}

// newCommandWiring assembles the CommandWiring (port of the corresponding InteractiveMode wiring).
func newCommandWiring(app *App) *CommandWiring {
	return &CommandWiring{
		Chat:        app.chat,
		UI:          app.ui,
		Settings:    app.settings,
		Session:     app.commandSession(),
		SessionInfo: app.sessionMgr,
		AppName:     app.options.AppName,
		Platform:    app.options.Platform,
		ShowStatus:  func(message string) { app.transcript.ShowStatus(message) },
		ShowError:   func(message string) { app.showError(message) },
		ShowWarning: func(message string) { app.showWarning(message) },
		ReloadGoal: func() {
			if controller := app.session.GoalController(); controller != nil {
				controller.Start("reload")
			}
		},
		RequestRender:        func() { app.ui.RequestRender(false) },
		ClearStatusIndicator: func() { app.uiState.ClearStatusIndicator("", false) },
		LoopScheduler:        app.loopScheduler,
		LoopMaxAge:           app.loopMaxAge,
		LoopFeatureEnabled:   app.settings.GetLoopFeatureEnabled,
		RunGoalCommand: func(args string) bool {
			controller := app.session.GoalController()
			if controller == nil {
				return false
			}
			controller.HandleCommand(args)
			return true
		},
		OnLoopPrompt: func(prompt string) {
			// The same path a typed message takes, so a loop's prompt is ordinary
			// user input rather than a special event.
			app.submit.HandleSubmit(context.Background(), prompt)
		},
		MarkdownTheme: func() tui.MarkdownTheme { return *app.markdownTheme() },
		DefaultSyncNotice: func() (string, bool) {
			result := app.session.LastDefaultSync()
			return result.Message, result.Warning
		},
		ExportQueue: app.offloopGroup.Queue(),
		PostToUI:    func(fn func()) { app.ui.Post(fn) },
		// Bound here, on the owner loop: the export worker must not read the
		// session wiring, which the loop rewrites on a session switch.
		Exporters: func() SessionExporters {
			session := app.session.AgentSession
			themeName := ""
			if themeSetting := app.settings.GetThemeSetting(); themeSetting != nil {
				themeName = *themeSetting
			}
			return SessionExporters{
				JSONL: session.ExportToJsonl,
				HTML: func(outputPath string) (string, error) {
					return ExportSessionToHTML(session, outputPath, themeName)
				},
			}
		},

		CopyToClipboard: app.copyToClipboardOffLoop,
		WriteDebugLog:   WriteDebugLogFile,
		EditorContainer: app.editorContainer,
		Editor:          app.defaultEditor,
		// `/new` starts a fresh session. The seam was never assigned, so the
		// command cleared the editor and returned without a word; the keybinding
		// (app.session.new) already used this implementation.
		// The dialogs the import flow asks: upstream reaches them through its
		// extension UI; here they are the selector slot with a callback.
		ShowExtensionConfirm: func(ctx context.Context, title string, message string, onAnswer func(confirmed bool)) {
			app.askConfirm(title, message, onAnswer)
		},
		PromptForMissingCwd: func(ctx context.Context, issue coding.SessionCwdIssue, onAnswer func(coding.MissingSessionCwdChoice)) {
			app.askMissingSessionCwd(issue, onAnswer)
		},
		ForkIntoSession: app.forkIntoSessionAsync,
		// `/import` copies a session file into this project's session directory and
		// switches to it.
		ImportFromJSONL: func(ctx context.Context, inputPath string, cwdOverride string, onDone func(bool, error)) {
			app.importFromJSONLAsync(ctx, inputPath, cwdOverride, func(result *SessionSwitchResult, err error) {
				onDone(result != nil && result.Cancelled, err)
			})
		},
		NewSession: func(ctx context.Context) (bool, error) {
			result, err := app.sessionNew()
			if err != nil {
				return false, err
			}
			return result.Cancelled, nil
		},
		HandleFatalRuntimeError: func(ctx context.Context, prefix string, err error) {
			app.trust.HandleFatalRuntimeError(ctx, prefix, err)
		},
		RunDetached: func(fn func()) bool {
			return app.runDetached(func(ctx context.Context) error { fn(); return nil })
		},
		ReloadNow: func() (string, bool, error) {
			// Upstream session.reload and then the mode's follow-ups: settings
			// re-read, queue modes, resource files and the system prompt (the
			// extension runner is out of scope, D41), keybindings, and implicit
			// project trust.
			app.session.Reload()
			app.keybindings.Reload()
			savedTrust := app.trust.MaybeSaveImplicitProjectTrustAfterReload(app.autoTrustOnReloadCwd)
			return app.session.ModelRuntime().GetError(), savedTrust, nil
		},
		// The MCP half of the reload: re-read mcp.json, close and restart the
		// manager's connections, wire the fresh manager (upstream session_start
		// reason "reload"). The loop resolves the cwd, trust and current-manager
		// snapshot here; the rebuild itself runs detached, and the result is
		// installed on the loop. The connection failures land in the wiring's
		// error report; the tools attach through the rebuilt manager's callbacks.
		ReloadMCP: func() ReloadMCPPlan {
			cwd := app.sessionMgr.GetCwd()
			trusted := app.settings != nil && app.settings.IsProjectTrusted()
			hadManager := app.mcpManager != nil
			return ReloadMCPPlan{
				Build: func(ctx context.Context) (*coding.McpManager, []string) {
					return app.buildReloadedMCPManager(ctx, cwd, trusted, hadManager)
				},
				Apply: func(manager *coding.McpManager) {
					// A switch while the build ran already installed the new
					// cwd's manager (upstream's session_start re-reads the
					// config for that session), so this result is stale.
					if manager != nil && app.sessionMgr.GetCwd() != cwd {
						app.retireMCPManager(manager)
						return
					}
					app.applyReloadedMCPManager(manager)
				},
			}
		},
		ApplyReloadedSettings: app.applyReloadedSettings}
}
