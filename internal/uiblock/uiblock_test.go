package uiblock

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// acceptedFindings is the allowlist for both invariant tests, keyed by the head
// of the reachability chain (the wiring field or root that pulls the work in). A
// new blocking call under an accepted surface is accepted with it, so the reason
// must say why the surface is safe. `lookedUp` also tries the surface with its
// kind and with its immediate callee, for a surface that is accepted for some
// kinds or some callees only.
var acceptedFindings = map[string]string{
	"wiring field OnExternalEditor": "the external editor takes over the terminal synchronously, by design (upstream identical)",
	"wiring field ShowAuthSelect":   "the auth flow invokes it on its own goroutine; it blocks waiting for the dialog, which renders on the loop",
	"wiring field TakeCrash":        "runs during Init, before the loop starts",
	"wiring field CheckVersion":     "invoked only from the notifyNewVersion goroutine (go w.notifyNewVersion)",
	"wiring field WriteDebugLog":    "user-invoked /debug; MkdirAll, one WriteFile and agent-dir path resolution, sub-millisecond",
	"(*github.com/dat267/pier/coding/interactive.EventDispatcher).HandleEvent -> github.com/dat267/pier/coding.SessionEntryToContextMessages": "one appended custom_message whose display flag is set, decoded so it can be rendered; sub-millisecond, and a new blocking call in the dispatcher still fails above",

	// Surfaces the gate only saw once it descended into pointer-receiver
	// methods (A493). Each reason names what was checked.
	"wiring field ReloadNow":            "off the loop in production: every call site is inside the closure handed to RunDetached (interactivemode_commands.go:374-397); the inline branch runs only when RunDetached is nil, which is the test-app composition",
	"wiring field ReloadMCP":            "off the loop in production, in the same RunDetached closure as ReloadNow; the inline branch is the test-app path",
	"wiring field ExportToHTML":         "off the loop: /export runs on the export queue (D441)",
	"wiring field WarnAnthropic":        "off the loop: interactivemode_run.go:523 calls it as `go w.WarnAnthropic(ctx)`, so its auth-storage reads and any `!command` credential run on that goroutine",
	"wiring field Stop":                 "shutdown: the crash/trust wiring invokes it as the app exits, after the loop stops",
	"wiring field ReloadGoal":           "goal status card: one text update, no I/O",
	"wiring field SwitchTuiMode":        "tui-mode switch: terminal-mode escape sequences and one render",
	"wiring field FlushCompactionQueue": "compaction queue flush: channel signals to workers, no I/O",
	"wiring field ClearStatusIndicator": "status clear: resets the indicator and posts a render",
	"wiring field RefreshModelCatalogs": "catalog refresh: schedules the existing background refresh and posts its result",
	"(*github.com/dat267/pier/coding/interactive.LoginDialogComponent).HandleTerminalInput": "login dialog keys: cancels a context and closes a channel",
	"(*github.com/dat267/pier/coding/interactive.RunWiring).Run":                            "the loop itself: its select is the idle wait (isLoopIdle) and its channel ops are the loop's own wiring",

	// Loop-side work with a measured or bounded cost.
	"via github.com/dat267/pier/coding/interactive.runSessionFileWork":                         "the post-back rebind runs on the loop by ownership (D442), and A494 moved its largest part off it: the projection decode now happens in the worker, 203.9 ms -> 63.6 ms at 12,500 turns, with the remainder transcript construction. The in-memory fork branch cannot reach the file write it lists, because a non-persisted session has no file",
	"(*github.com/dat267/pier/coding/interactive.FooterComponent).Render|cpu-bound":            "the context estimate is cached by projection version (M139), so a build follows a session change rather than a frame",
	"(*github.com/dat267/pier/coding/interactive.TreeList).Render|cpu-bound":                   "A496 memoized each row's decode per entry; the walk follows the now-cold extraction path",
	"(*github.com/dat267/pier/coding/interactive.ToolExecutionComponent).Invalidate|cpu-bound": "captureBuiltinResult only runs for read/bash results of 64 KiB or more, and clones at most 16 KiB of details",
	"(*github.com/dat267/pier/coding/interactive.EventDispatcher).HandleEvent":                 "one decode per appended entry, on the event that carries it, not per frame",
	"wiring field Prompt":     "the compaction check appends a context edit through the session write queue (A483, A485); the writer the walk lists is the queue's own path for a session with no queue, which production never has",
	"wiring field NewSession": "one EnsurePrivateSessionDir mkdir plus the same rebind a switch performs (see the runSessionFileWork entry)",
	"(*github.com/dat267/pier/tui.Renderer).HandleTerminalInput -> (*github.com/dat267/pier/tui.Editor).HandleInput (interface HandleInput)":                              "the inline provider call is the branch for a host that does not implement EditorPoster, which production's editorHostAdapter does (app.go:1622 posts to the owner loop); the production request debounces, hands the provider to its own goroutine and posts the result back for the stale-token check",
	"(*github.com/dat267/pier/tui.Renderer).HandleTerminalInput -> (*github.com/dat267/pier/coding/interactive.TreeList).HandleInput (interface HandleInput)":             "tree search filters over text memoized per node (A496); the search itself is a string compare per visible row",
	"(*github.com/dat267/pier/tui.Renderer).HandleTerminalInput -> (*github.com/dat267/pier/tui.terminalQueries).ConsumeInput":                                            "terminal query parser: consumes buffered bytes already read from the terminal, no I/O",
	"(*github.com/dat267/pier/tui.Renderer).HandleTerminalInput -> (*github.com/dat267/pier/coding/interactive.LoginDialogComponent).HandleInput (interface HandleInput)": "escape in the login dialog closes its cancel channel; a close cannot block",
	"(*github.com/dat267/pier/coding/interactive.Lifecycle).HandleSignal":                                                                                                 "signal handling: Shutdown drains buffered terminal input with a bounded wait before stopping the screens",
	"(*github.com/dat267/pier/tui.Editor).Render|syscall/io":                                                                                                              "Rows queries the terminal only while the stored height is still zero, which is before Start sets it, so this is a once-only ioctl rather than a per-frame one",
	"wiring field OnFollowUp -> (*github.com/dat267/pier/coding/interactive.QueueController).HandleFollowUp":                                                              "the same interface over-approximation as OnModelCycleBackward: the rpcclient Prompt is never wired in the TUI",
	"wiring field Shutdown":                "shutdown: drains buffered terminal input with a bounded wait before the screens stop (the same drain as Lifecycle.HandleSignal)",
	"wiring field OnExit":                  "shutdown: the same bounded terminal-input drain as Shutdown",
	"wiring field TerminalRows":            "the same once-only size query as the editor: Rows falls back to a syscall only before Start stores the size",
	"wiring field RebuildChatFromMessages": "the startup chat rebuild walks the compaction window per message, on the loop by ownership; bounded by the transcript the app was just given",
	"wiring field ShowStatusIndicator":     "the status indicator disposes the previous footer data provider, which only closes a channel",
	"wiring field RenderInitialMessages":   "the transcript build runs on the loop after a rebind by ownership (D442); A494 measured the whole rebind at 63.6 ms for 12,500 turns after the projection moved to the worker, and this walk is part of that remainder",
	"(*github.com/dat267/pier/coding/interactive.ToolExecutionComponent).Render|cpu-bound": "Render only rebuilds while arguments are still streaming (argsPending), so this decode runs per streamed delta, not per frame",
	"wiring field OnThinkingToggle": "a settings toggle clones the settings map before the queued write; the write itself runs on the settings persist queue, and the clone is one small struct",
}

// TestNothingBlockingReachableFromTheUIGoroutine holds the invariant that no
// blocking work runs on the main event loop. Every finding is a call site that
// can block the loop — syscalls, network, exec, blocking channel operations,
// locks, sleeps, or CPU-bound JSON — reachable from the loop's phases, the
// components it renders, or the closures marshaled onto it. New violations
// fail here with the chain that reaches them; accepted sites get an explicit
// allowlist entry with a reason.
func TestNothingBlockingReachableFromTheUIGoroutine(t *testing.T) {
	if testing.Short() {
		t.Skip("loads and builds SSA for the whole module")
	}
	findings, err := Find("../..")
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}
	allowlist := acceptedFindings
	for _, f := range findings {
		if reason, ok := lookedUp(allowlist, f); ok {
			t.Logf("allowed: %s %s (%s)", f.Kind, f.Pos, reason)
			continue
		}
		t.Errorf("%s %s\n  chain: %s", f.Kind, f.Pos, strings.Join(f.Chain, " -> "))
	}
}

// lookedUp finds an allowlist entry for a finding. Keys, most specific first: the
// surface and kind, the surface and its immediate callee, the surface, and any
// prefix of the surface (a generic instantiation carries a type argument).
func lookedUp(allowlist map[string]string, finding Finding) (string, bool) {
	surface := finding.Chain[0]
	candidates := []string{surface + "|" + finding.Kind.String()}
	if len(finding.Chain) > 1 {
		candidates = append(candidates, surface+" -> "+finding.Chain[1])
	}
	candidates = append(candidates, surface)
	for key, reason := range allowlist {
		if reason != "" && strings.HasPrefix(surface, key+"[") {
			candidates = append(candidates, key)
		}
	}
	for _, key := range candidates {
		if reason, ok := allowlist[key]; ok {
			return reason, true
		}
	}
	return "", false
}

// goroutineKey names a goroutine finding by its file and the goroutine's own
// function. A line number moves whenever the surrounding code changes, which
// once broke the gate for an edit that did not touch the goroutine.
func goroutineKey(f Finding) string {
	file := f.Pos
	if index := strings.LastIndex(file, "/"); index >= 0 {
		file = file[index+1:]
	}
	if index := strings.Index(file, ":"); index >= 0 {
		file = file[:index]
	}
	if len(f.Chain) == 0 {
		return f.Pos
	}
	return file + " " + strings.TrimPrefix(f.Chain[len(f.Chain)-1], "go ")
}

// TestBareBlockingGoroutinesAreAllowlisted lists `go` statements reachable
// from the UI loop whose goroutine body does blocking work. A goroutine does
// not block the loop — the loop never waits on it — so this is not the
// invariant above; it is a nudge toward internal/offloop, which supplies the
// submission order, coalescing and shutdown drain a bare goroutine lacks.
// Accepted sites get an allowlist entry with a reason.
func TestBareBlockingGoroutinesAreAllowlisted(t *testing.T) {
	if testing.Short() {
		t.Skip("loads and builds SSA for the whole module")
	}
	findings, err := FindGoroutines("../..")
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}
	// Background helpers the loop only starts: a goroutine never blocks the loop,
	// and these are deliberate workers with their own lifetime. Keys name the file
	// and the goroutine function, so an unrelated edit that moves the line does not
	// break the gate.
	allowlist := map[string]string{
		"authstorage.go (*github.com/dat267/pier/coding.AuthStorage).readLatestData$1":                                                 "a credential read's watcher goroutine, started per read and finished by it",
		"authstorage.go (*github.com/dat267/pier/coding.authFileLock).refresh":                                                         "the auth lock's refresh goroutine, which keeps a held lock alive (its whole purpose)",
		"catalogrefresh.go (*github.com/dat267/pier/coding/interactive.ModelCatalogRefreshCoordinator).run":                            "the catalog refresh coordinator's own worker goroutine, which joins concurrent refreshes and lives until they finish",
		"interactivemode_models.go (*github.com/dat267/pier/coding/interactive.StartupWiring).MaybeWarnAboutAnthropicSubscriptionAuth": "the model-selected handler starts the startup auth warning as a goroutine; the loop does not wait for it",
		"modelruntime.go (*github.com/dat267/pier/coding.ModelRuntime).runAvailabilityRefresh$1":                                       "the model runtime's availability refresh goroutine, started by Refresh and finished by it",
		"models_runtime.go (*github.com/dat267/pier/ai.Models).Refresh$1":                                                              "the model runtime's own background catalog refresh (upstream refreshes on create)",
		"modelsstore.go (*github.com/dat267/pier/coding.FileModelsStore).readLatest$1":                                                 "the models store's read watcher goroutine, started per read and finished by it",
		"sdk.go github.com/dat267/pier/coding.CreateAgentSession$10":                                                                   "CreateAgentSession loads context files, skills, prompt templates and overrides on four parallel goroutines and waits for all of them; the calls come from the D442 rebind, which is off the loop (A494)",
		"sdk.go github.com/dat267/pier/coding.CreateAgentSession$7":                                                                    "CreateAgentSession loads context files, skills, prompt templates and overrides on four parallel goroutines and waits for all of them; the calls come from the D442 rebind, which is off the loop (A494)",
		"sdk.go github.com/dat267/pier/coding.CreateAgentSession$8":                                                                    "CreateAgentSession loads context files, skills, prompt templates and overrides on four parallel goroutines and waits for all of them; the calls come from the D442 rebind, which is off the loop (A494)",
		"sdk.go github.com/dat267/pier/coding.CreateAgentSession$9":                                                                    "CreateAgentSession loads context files, skills, prompt templates and overrides on four parallel goroutines and waits for all of them; the calls come from the D442 rebind, which is off the loop (A494)",
		"settingsmanager.go github.com/dat267/pier/coding.maintainSettingsLock$1":                                                      "the settings lock's maintenance goroutine, which holds the lock alive while it is taken",
		"skills.go github.com/dat267/pier/coding.scanSkillTree$1":                                                                      "the skill scan's parallel directory walk; the caller waits for it",
		"terminal.go (*github.com/dat267/pier/tui.ProcessTerminal).SetProgress$1":                                                      "the terminal's progress-reporting goroutine for the current OSC 9;4 state",
		"terminal.go (*github.com/dat267/pier/tui.ProcessTerminal).writeLoop":                                                          "the terminal's write loop, which drains queued output frames",
		"terminal.go (*github.com/dat267/pier/tui.stdinReader).read":                                                                   "the terminal's input reader, the source of every key the loop handles",
		"transport_stdio.go (*github.com/dat267/pier/mcp.StdioTransport).pumpStderr":                                                   "the stdio transport's stderr pump, one per connection, ended when the server exits",
		"transport_stdio.go (*github.com/dat267/pier/mcp.StdioTransport).pumpStdout":                                                   "the stdio transport's stdout pump, the connection's only reader",
		"transport_stdio.go (*github.com/dat267/pier/mcp.StdioTransport).watchExit":                                                    "the stdio transport's exit watcher, which ends the connection when the server exits",
		"transport_stdio.go github.com/dat267/pier/mcp.installExitHook$1":                                                              "the process exit hook's goroutine, which runs when the process exits",
	}
	for _, f := range findings {
		if reason, ok := allowlist[goroutineKey(f)]; ok {
			t.Logf("allowed: %s %s (%s)", f.Kind, f.Pos, reason)
			continue
		}
		t.Errorf("%s %s: blocking work on a bare goroutine reachable from the UI loop; use internal/offloop\n  chain: %s",
			f.Kind, f.Pos, strings.Join(f.Chain, " -> "))
	}
}

// TestGoroutineBlockClassifierHasTeeth guards the check above from silently
// passing because the classifier never fires: at least one pier goroutine body
// in the module must be classified as blocking.
func TestGoroutineBlockClassifierHasTeeth(t *testing.T) {
	if testing.Short() {
		t.Skip("loads and builds SSA for the whole module")
	}
	m, err := cachedModule("../..")
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}
	for fn := range m.all {
		if fn.Blocks == nil || !isPier(fn) {
			continue
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				goIns, ok := instr.(*ssa.Go)
				if !ok {
					continue
				}
				if callee := goIns.Common().StaticCallee(); callee != nil && isPier(callee) && goroutineBlocks(callee, m) {
					return
				}
			}
		}
	}
	t.Fatal("no pier goroutine body classified as blocking; the classifier is likely broken")
}
