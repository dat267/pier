package interactive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// SwitchSession replaces the running session with the one at sessionPath
// (upstream AgentSessionRuntime.switchSession plus the interactive-mode
// rebind).
func (a *App) switchSession(ctx context.Context, sessionPath string, cwdOverride string) (*SessionSwitchResult, error) {
	var result *SessionSwitchResult
	var err error
	runSessionFileWork(nil, nil, sessionFileWorkKey, a.openSessionWork(sessionPath, cwdOverride), func(opened *coding.SessionManager, openErr error) {
		result, err = a.applyOpenedSession(opened, openErr)
	})
	return result, err
}

// switchSessionAsync opens the session off the owner loop and rebinds on it.
func (a *App) switchSessionAsync(_ context.Context, sessionPath string, cwdOverride string, done func(*SessionSwitchResult, error)) {
	runSessionFileWork(a.sessionQueue, a.postToOwnerLoop, sessionFileWorkKey,
		a.openSessionWork(sessionPath, cwdOverride), func(opened *coding.SessionManager, openErr error) {
			done(a.applyOpenedSession(opened, openErr))
		})
}

// warmSessionProjection derives the session projection on the worker that built
// the manager. Projection() caches by version, so the rebind that runs on the
// owner loop reads the cache instead of decoding every entry: at 12,500 turns
// that decode was 151 ms of a 204 ms rebind, and warming it in the worker cut the
// loop half from 204 ms to 67 ms (A494). The manager is new here, so no reader
// can be racing this pass.
func warmSessionProjection(manager *coding.SessionManager) *coding.SessionManager {
	if manager != nil {
		_ = manager.Projection()
	}
	return manager
}

// openSessionWork parses the session file and checks its stored cwd; it is the
// part that runs on a worker.
func (a *App) openSessionWork(sessionPath string, cwdOverride string) func(context.Context) (*coding.SessionManager, error) {
	return func(context.Context) (*coding.SessionManager, error) {
		sessionManager, err := coding.OpenSession(sessionPath, "", cwdOverride)
		if err != nil {
			return nil, err
		}
		if err := coding.AssertSessionCwdExists(sessionManager, a.options.Cwd); err != nil {
			return nil, err
		}
		return warmSessionProjection(sessionManager), nil
	}
}

// applyOpenedSession rebinds the UI to an opened session. It touches attached
// state, so it runs on the owner loop.
func (a *App) applyOpenedSession(sessionManager *coding.SessionManager, openErr error) (*SessionSwitchResult, error) {
	if openErr != nil {
		return nil, openErr
	}
	// The previous session's run is gone, so its working or done status must not linger over
	// the new one (upstream resets the reporter when the session is bound).
	if a.programStatus != nil {
		a.programStatus.Reset()
	}
	return a.applySessionReplacement(sessionManager)
}

// SessionImportFileNotFoundError is returned when /import names a JSONL path
// that does not exist (upstream agent-session-runtime.ts
// SessionImportFileNotFoundError; the command reports it without exiting).
type SessionImportFileNotFoundError struct{ FilePath string }

func (e *SessionImportFileNotFoundError) Error() string { return "File not found: " + e.FilePath }

// importFromJSONLAsync copies and opens the import off the owner loop: the file
// is copied rather than opened in place so the imported session becomes part of
// this project's session list, and an existing session with the same name is
// never clobbered because the copy takes a numbered name instead (upstream
// AgentSessionRuntime.importFromJsonl).
func (a *App) importFromJSONLAsync(_ context.Context, inputPath string, cwdOverride string, done func(*SessionSwitchResult, error)) {
	runSessionFileWork(a.sessionQueue, a.postToOwnerLoop, sessionFileWorkKey,
		a.importSessionWork(inputPath, cwdOverride), func(opened *coding.SessionManager, openErr error) {
			done(a.applyOpenedSession(opened, openErr))
		})
}

// importSessionWork copies the file into the session directory and opens it.
func (a *App) importSessionWork(inputPath string, cwdOverride string) func(context.Context) (*coding.SessionManager, error) {
	return func(context.Context) (*coding.SessionManager, error) {
		resolved := coding.ResolvePath(inputPath, "", coding.PathInputOptions{})
		if _, err := os.Stat(resolved); err != nil {
			return nil, &SessionImportFileNotFoundError{FilePath: resolved}
		}

		sessionDir := a.sessionMgr.GetSessionDir()
		if sessionDir == "" || !a.sessionMgr.SessionDirManaged() {
			// An in-memory session (--no-session) has no directory of its own yet,
			// and a session opened by path only inherits its file's directory:
			// an import lands in the default store, never in a directory pier does
			// not own (M73).
			sessionDir = coding.DefaultSessionDir(a.sessionMgr.GetCwd(), "")
		}
		if err := coding.EnsurePrivateSessionDir(sessionDir); err != nil {
			return nil, err
		}
		destination := filepath.Join(sessionDir, filepath.Base(resolved))
		alreadyStored := coding.PathsEqual(destination, resolved)
		if !alreadyStored {
			extension := filepath.Ext(destination)
			stem := strings.TrimSuffix(destination, extension)
			for suffix := 1; ; suffix++ {
				if _, err := os.Stat(destination); err != nil {
					break
				}
				destination = fmt.Sprintf("%s-%d%s", stem, suffix, extension)
			}
			if err := copyFileExclusive(resolved, destination); err != nil {
				return nil, err
			}
		}

		sessionManager, err := coding.OpenSession(destination, sessionDir, cwdOverride)
		if err != nil {
			return nil, err
		}
		if err := coding.AssertSessionCwdExists(sessionManager, a.options.Cwd); err != nil {
			return nil, err
		}
		return warmSessionProjection(sessionManager), nil
	}
}

// copyFileExclusive copies src to dst, refusing to overwrite. Unlike upstream's
// COPYFILE_EXCL, the destination retains owner-only permissions (D289).
// Failed copies remove their newly created destination when possible (D371).
func copyFileExclusive(src string, dst string) (err error) {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	destination, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return err
	}
	return destination.Close()
}

// ForkedSessionResult is the outcome of forking a session into another cwd and
// switching to it (upstream switchToForkedSession).
type ForkedSessionResult struct {
	SessionID string
	Cwd       string
	Cancelled bool
}

// Status is upstream's status line after a successful fork.
func (r *ForkedSessionResult) Status() string {
	return "Forked into new session " + r.SessionID + " in " + r.Cwd
}

// forkIntoSessionAsync forks the session at sourcePath into cwd and switches to
// the fork (upstream InteractiveMode.switchToForkedSession, #11060). A vetoed
// switch removes the fork, so cancelling leaves no copy behind.
func (a *App) forkIntoSessionAsync(ctx context.Context, sourcePath string, cwd string, done func(*ForkedSessionResult, error)) {
	// Snapshot the target directory on the owner loop; the fork writes the whole
	// log off it.
	sessionDir := a.sessionMgr.GetSessionDir()
	fork := coding.ForkSession
	if !a.sessionMgr.SessionDirManaged() {
		fork = coding.ForkSessionDerived
	}
	work := func(context.Context) (*coding.SessionManager, error) {
		return fork(sourcePath, cwd, sessionDir, nil)
	}
	runSessionFileWork(a.sessionQueue, a.postToOwnerLoop, sessionFileWorkKey, work, func(forked *coding.SessionManager, forkErr error) {
		if forkErr != nil {
			done(nil, forkErr)
			return
		}
		forkPath := forked.GetSessionFile()
		if a.sessions.SwitchSession == nil {
			_ = os.Remove(forkPath)
			done(&ForkedSessionResult{Cancelled: true}, nil)
			return
		}
		a.sessions.SwitchSession(ctx, forkPath, "", func(result *SessionSwitchResult, err error) {
			if err != nil {
				done(nil, err)
				return
			}
			if result != nil && result.Cancelled {
				// An extension vetoed the switch; drop the fork so cancelling
				// leaves no copy behind.
				_ = os.Remove(forkPath)
				done(&ForkedSessionResult{Cancelled: true}, nil)
				return
			}
			done(&ForkedSessionResult{SessionID: forked.GetSessionID(), Cwd: cwd}, nil)
		})
	})
}

// SessionNew starts a fresh session in the current session directory (upstream
// AgentSessionRuntime.newSession).
func (a *App) sessionNew(ctx context.Context) (*SessionSwitchResult, error) {
	sessionDir := a.sessionMgr.GetSessionDir()
	persist := a.sessionMgr.IsPersisted()
	sessionManager := coding.NewSessionManager(a.sessionMgr.GetCwd(), &coding.SessionManagerOptions{
		SessionDir:        sessionDir,
		SessionDirDerived: !a.sessionMgr.SessionDirManaged(),
		Persist:           &persist,
		WriteQueue:        a.sessionMgr.GetWriteQueue(),
	})
	return a.applySessionReplacement(sessionManager)
}

// applySessionReplacement tears down the current session, builds a new agent
// session around sessionManager and rebinds the UI (upstream teardownCurrent +
// createRuntime + rebindCurrentSession; the extension lifecycle events are
// no-ops in this port, D41).
func (a *App) applySessionReplacement(sessionManager *coding.SessionManager) (*SessionSwitchResult, error) {
	// A485: every replacement path (switch, fork, import, new) must keep the
	// write queue. OpenSession builds a manager without one, and nil means the
	// session file is written synchronously on the calling goroutine, which for
	// this path is the owner loop. cmd/main.go wires the queue for the same
	// reason; sessionNew forwards it explicitly and the others did not.
	if sessionManager != nil && sessionManager.GetWriteQueue() == nil {
		if queue := a.sessionMgr.GetWriteQueue(); queue != nil {
			sessionManager.SetWriteQueue(queue)
		}
	}
	a.teardownCurrentSession()
	return a.bindSessionManager(sessionManager)
}

func (a *App) teardownCurrentSession() {
	// Retire event admission before waiting: an active producer may be parked
	// on the lossless queue that this same owner is no longer draining.
	if a.unsubscribe != nil {
		a.unsubscribe()
		a.unsubscribe = nil
	}
	a.session.Abort(context.Background())
	a.session.Dispose()
	// The working period and the indicator that showed it belonged to the
	// session going away: drop both, so nothing works, spins or adds the old
	// run's time to the new session's first notice (D402). Upstream's own
	// replacement paths begin with clearStatusIndicator
	// (interactive-mode.ts handleResumeSession, handleImportCommand,
	// handleClearCommand, stop).
	if a.uiState != nil {
		a.uiState.DiscardWorkingPeriod()
		a.uiState.ClearStatusIndicator("", false)
	}
	// The loop is running this replacement, so events the outgoing session
	// queued before its listener was removed are still pending. The incoming
	// transcript is rebuilt from persisted entries; applying those stale
	// messages or status changes would duplicate or overwrite it.
	a.sessionEvents.Clear()
}

func (a *App) bindSessionManager(sessionManager *coding.SessionManager) (*SessionSwitchResult, error) {
	// The runtime is cwd-bound: upstream's createRuntime resolves the session's
	// cwd's project trust and builds a settings manager for it, so a session from
	// another directory gets that directory's settings, resources and trust
	// rather than this project's. Resolving here — without a prompt, because
	// upstream passes hasUI false for every runtime but the initial one — and
	// re-pointing the one settings manager (D160) keeps every holder consistent.
	if err := a.rebindProjectSettings(sessionManager.GetCwd()); err != nil {
		return nil, err
	}

	created, err := coding.CreateAgentSession(context.Background(), &coding.CreateAgentSessionOptions{
		Cwd:             sessionManager.GetCwd(),
		AgentDir:        a.options.AgentDir,
		SessionManager:  sessionManager,
		ModelRuntime:    a.runtime,
		SettingsManager: a.settings,
	})
	if err != nil {
		return nil, err
	}

	// The *AppSession pointer is shared by every wiring, so swapping the
	// embedded AgentSession rebinds them all at once.
	a.session.AgentSession = created.Session
	a.wireGoalController()
	a.sessionMgr = sessionManager
	// Upstream's createRuntime builds a fresh extension runtime for every bind,
	// so the incoming session_start re-reads mcp.json for the new cwd and
	// reconnects. Reattaching the outgoing manager instead would keep tools
	// from the old config and miss servers added while the app was running.
	// The config errors are shown after the transcript is rebuilt below.
	mcpErrors := a.reloadMCP(context.Background(), sessionManager.GetCwd())
	a.footerData.SetCwd(sessionManager.GetCwd())
	a.transcript.SessionInfo = sessionManager
	a.events.SessionInfo = sessionManager
	a.startup.SessionInfo = sessionManager
	a.sessions.SessionInfo = sessionManager
	a.commands.SessionInfo = sessionManager
	a.selectors.SessionInfo = sessionManager
	a.trust.SessionInfo = sessionManager
	a.autocomplete.SessionInfo = sessionManager

	if a.unsubscribe == nil {
		generation, enqueue, stopProducer := a.sessionEvents.newProducer()
		a.events.sessionGeneration = generation
		unsubscribe := a.session.Subscribe(enqueue)
		a.unsubscribe = func() {
			stopProducer()
			unsubscribe()
		}
	}
	a.footer.Invalidate()
	a.lifecycle.UpdateTerminalTitle()
	// upstream rebindCurrentSession({renderBeforeBind: true}) → the session's own
	// reset, which is the initial render and not the reload's rebuild.
	a.startup.RenderCurrentSessionState()
	// renderCurrentSessionState clears the loaded-resources container; upstream
	// rebuilds it in bindCurrentSessionExtensions, whose last step is
	// showLoadedResources. Without it, /new and a resume leave Context/Skills
	// blank until the next /reload.
	a.showLoadedResources(false)
	for _, message := range mcpErrors {
		a.showWarning(message)
	}
	a.ui.RequestRender(false)
	return &SessionSwitchResult{}, nil
}

// wireGoalController restores the idle-round launcher and notifications on
// every bind (construction, /new, /resume, /fork). Agent-end effects run on a
// worker, so UI notifications are always posted to the owner (D143/D223).
func (a *App) wireGoalController() {
	if controller := a.session.Goal; controller != nil {
		controller.SetStartWork(a.events.StartWork)
		controller.SetUIHooks(func(message string, level string) {
			a.ui.Post(func() {
				if a.session.Goal != controller {
					return
				}
				if level == "warning" {
					a.showWarning(message)
					return
				}
				a.transcript.ShowStatus(message)
			})
		})
	}
}

// rebindProjectSettings re-points the settings manager at a session's project,
// resolving that project's trust first. A switch within the same project, or one
// with no directory to resolve, keeps the current settings.
func (a *App) rebindProjectSettings(cwd string) error {
	if cwd == "" || coding.PathsEqual(cwd, a.settings.Cwd()) {
		return nil
	}
	key := coding.CanonicalizePath(coding.ResolvePath(cwd, "", coding.PathInputOptions{}))
	if known, ok := a.projectTrustByCwd[key]; ok {
		a.applyProjectSettings(cwd, known)
		return nil
	}
	trusted, err := coding.ResolveProjectTrusted(coding.ResolveProjectTrustedOptions{
		Cwd:                 cwd,
		TrustStore:          coding.NewProjectTrustStore(a.options.AgentDir),
		TrustOverride:       a.options.ProjectTrustOverride,
		DefaultProjectTrust: a.settings.GetDefaultProjectTrust(),
		// No UI: a switch is never the startup prompt (upstream's
		// projectTrustContextFactory reports hasUI false off the initial runtime),
		// so an undecided project stays untrusted.
		ProjectTrustContext: coding.ProjectTrustContext{},
	})
	if err != nil {
		return err
	}
	a.projectTrustByCwd[key] = trusted
	a.applyProjectSettings(cwd, trusted)
	return nil
}

// applyProjectSettings points the settings manager at a project under a decided
// trust and re-applies what follows from the swap.
func (a *App) applyProjectSettings(cwd string, trusted bool) {
	a.settings.RebindProject(cwd, trusted)
	// The run's own override is not part of the project scope, so re-apply it
	// (upstream applies the CLI overrides to the runtime settings manager).
	if a.options.InitialThemeSetting != nil {
		a.settings.ApplyOverrides(&coding.Settings{Theme: a.options.InitialThemeSetting})
	}
	// Settings-derived UI state follows the manager (upstream rebindCurrentSession
	// calls applyRuntimeSettings).
	a.applySettingsDependentUI()
}

// forkAtEntry is the runtime's fork (upstream AgentSessionRuntime.fork), shared
// by /clone and by forking from a user message: it branches a new session at the
// given entry and switches the app to it.
//
// The two positions are upstream's: "at" keeps the entry (clone at the current
// position, so the new leaf is that entry), while "before" requires a user
// message, keeps its parent as the leaf and hands the message text back so the
// editor can send it again.
func (a *App) forkAtEntry(_ context.Context, entryID string, atPosition bool) (*SelectorForkResult, error) {
	var result *SelectorForkResult
	var err error
	runSessionFileWork(nil, nil, sessionFileWorkKey, a.forkWork(entryID, atPosition), func(outcome *forkOutcome, workErr error) {
		result, err = a.applyFork(outcome, workErr)
	})
	return result, err
}

// forkAtEntryAsync writes the forked session off the owner loop and rebinds on
// it. Writing a fork serializes the whole branch: 519ms on a 25,583-entry
// session.
func (a *App) forkAtEntryAsync(_ context.Context, entryID string, atPosition bool, done func(*SelectorForkResult, error)) {
	runSessionFileWork(a.sessionQueue, a.postToOwnerLoop, sessionFileWorkKey,
		a.forkWork(entryID, atPosition), func(outcome *forkOutcome, workErr error) {
			done(a.applyFork(outcome, workErr))
		})
}

// forkOutcome is what the worker produces: a forked manager for a persisted
// session, or the branch point to cut in memory.
type forkOutcome struct {
	forked       *coding.SessionManager
	selectedText *string
	inMemory     bool
	targetLeafID string
}

func (a *App) forkWork(entryID string, atPosition bool) func(context.Context) (*forkOutcome, error) {
	return func(context.Context) (*forkOutcome, error) {
		entry := a.sessionMgr.GetEntry(entryID)
		if entry == nil {
			return nil, errors.New("Invalid entry ID for forking")
		}

		targetLeafID := entryID
		var selectedText *string
		if !atPosition {
			text, ok := userMessageEntryText(entry)
			if !ok {
				return nil, errors.New("Invalid entry ID for forking")
			}
			selectedText = &text
			targetLeafID = ""
			if entry.ParentID != nil {
				targetLeafID = *entry.ParentID
			}
		}

		if !a.sessionMgr.IsPersisted() {
			return &forkOutcome{inMemory: true, targetLeafID: targetLeafID, selectedText: selectedText}, nil
		}
		fork := coding.ForkSessionAtEntryDerived
		if a.sessionMgr.SessionDirManaged() {
			fork = coding.ForkSessionAtEntry
		}
		forked, err := fork(a.sessionMgr, targetLeafID, a.sessionMgr.GetCwd(), a.sessionMgr.GetSessionDir(), nil)
		if err != nil {
			return nil, err
		}
		return &forkOutcome{forked: warmSessionProjection(forked), selectedText: selectedText}, nil
	}
}

// applyFork rebinds the UI to the forked session (owner loop only).
func (a *App) applyFork(outcome *forkOutcome, workErr error) (*SelectorForkResult, error) {
	if workErr != nil {
		return nil, workErr
	}
	var result *SessionSwitchResult
	if outcome.inMemory {
		manager := a.sessionMgr
		a.teardownCurrentSession()
		if outcome.targetLeafID == "" {
			manager.NewSession(nil)
		} else if _, err := manager.CreateBranchedSession(outcome.targetLeafID); err != nil {
			return nil, err
		}
		bound, err := a.bindSessionManager(manager)
		if err != nil {
			return nil, err
		}
		result = bound
	} else {
		applied, err := a.applySessionReplacement(outcome.forked)
		if err != nil {
			return nil, err
		}
		result = applied
	}
	return &SelectorForkResult{Cancelled: result.Cancelled, SelectedText: outcome.selectedText}, nil
}

// tailSessionAsync seeds a new session with the current live context off the
// owner loop (D378): the newest compaction plus the entries its window keeps,
// re-rooted so the compacted prefix stops traveling with the session, written the
// same way a fork does. Upstream has no such command; its only seeding path is
// the extension setup hook, which is out of scope (D41). A session that does not
// persist has no file to tail from, so the command reports instead of silently
// starting an empty one.
func (a *App) tailSessionAsync(_ context.Context, done func(*SessionSwitchResult, error)) {
	runSessionFileWork(a.sessionQueue, a.postToOwnerLoop, sessionFileWorkKey, a.tailWork(),
		func(tailed *coding.SessionManager, workErr error) {
			done(a.applyOpenedSession(tailed, workErr))
		})
}

func (a *App) tailWork() func(context.Context) (*coding.SessionManager, error) {
	return func(context.Context) (*coding.SessionManager, error) {
		if !a.sessionMgr.IsPersisted() {
			return nil, errors.New("Tail needs a saved session")
		}
		tailed, err := coding.TailSession(a.sessionMgr, a.session.Model(), a.session.ThinkingLevel(),
			a.sessionMgr.GetCwd(), a.sessionMgr.GetSessionDir(), a.sessionMgr.SessionDirManaged(), nil)
		if err != nil {
			return nil, err
		}
		return warmSessionProjection(tailed), nil
	}
}

// userMessageEntryText returns a message entry's user text, and whether the
// entry is a user message at all.
func userMessageEntryText(entry *coding.SessionEntry) (string, bool) {
	if entry.Type != "message" {
		return "", false
	}
	message, err := ai.UnmarshalMessage(entry.Message)
	if err != nil {
		return "", false
	}
	user, ok := message.(*ai.UserMessage)
	if !ok {
		return "", false
	}
	return ai.ContentText(user.Content, ""), true
}
