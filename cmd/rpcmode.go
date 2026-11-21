package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/coding/interactive"
)

// runRPCSession follows pi v1.4.0 rpc-mode.ts signal cleanup. Return the
// status through run so MCP, settings and accepted-write defers still execute.
// replace builds a session for a session-replacement command; nil disables
// them. themeName seeds the export_html theme (empty uses the active theme).
func runRPCSession(parent context.Context, session *coding.AgentSession, replace func(context.Context, *coding.SessionManager) (*coding.AgentSession, error), themeName string) error {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	if runtime.GOOS == "windows" {
		signal.Notify(signals, syscall.SIGTERM)
	} else {
		signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP)
	}
	stop := make(chan struct{})
	done, exitCode := watchRPCSignals(signals, cancel, func(code int) { os.Exit(code) }, stop)
	err := coding.RunRPCMode(ctx, session, coding.RPCModeOptions{
		Input: os.Stdin, Output: os.Stdout, Stderr: os.Stderr, ReplaceSession: replace,
		ExportToHtml: func(exported *coding.AgentSession, outputPath string) (string, error) {
			return interactive.ExportSessionToHTML(exported, outputPath, themeName)
		},
	})
	signal.Stop(signals)
	close(stop)
	cancel()
	<-done
	if code := exitCode(); code != 0 {
		return reportedExitCode(code)
	}
	return err
}

// watchRPCSignals runs the RPC mode's signal protocol (pi v1.5.2 b4afdddf
// modes/rpc/rpc-mode.ts registerSignalHandlers). The first signal starts the
// shutdown by cancelling the session; a repeated one takes upstream's
// `signalCount > 1` branch and exits immediately, because a shutdown can wait on
// work such as an MCP token refresh. Handlers stay registered until stop closes,
// so a signal during the shutdown is still seen.
func watchRPCSignals(signals <-chan os.Signal, cancel func(), forceExit func(int), stop <-chan struct{}) (<-chan struct{}, func() int) {
	done := make(chan struct{})
	code := 0
	go func() {
		defer close(done)
		shuttingDown := false
		for {
			select {
			case sig := <-signals:
				requested := 143
				if sig == syscall.SIGHUP {
					requested = 129
				}
				if shuttingDown {
					forceExit(requested)
					continue
				}
				shuttingDown = true
				code = requested
				cancel()
			case <-stop:
				return
			}
		}
	}()
	return done, func() int { return code }
}

// rpcSessionReplacer builds the session factory for the RPC session-replacement
// commands (upstream AgentSessionRuntime.createRuntime). The new session shares
// the model runtime, settings and tool selection; the cwd and session manager
// come from the target. A cross-project switch resolves that project's trust
// without a prompt (upstream reports hasUI false) and rebinds the one settings
// manager (D160).
// rpcSessionReplacer builds the factory for a session-replacement command.
// recomputeScope re-resolves a settings-derived model scope after the project
// rebind (nil when --models named the scope), and reported collects the scope
// diagnostics already written, so a repeated warning is not printed again.
func rpcSessionReplacer(base *coding.CreateAgentSessionOptions, settings *coding.SettingsManager, agentDir string, trustOverride *bool, setMCPSession func(*coding.AgentSession), recomputeScope func(context.Context) ([]coding.ScopedModel, []coding.ModelScopeDiagnostic, error), reported map[string]bool, stderr io.Writer) func(context.Context, *coding.SessionManager) (*coding.AgentSession, error) {
	if reported == nil {
		reported = map[string]bool{}
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return func(ctx context.Context, manager *coding.SessionManager) (*coding.AgentSession, error) {
		cwd := manager.GetCwd()
		if cwd != "" && cwd != settings.Cwd() {
			trusted, err := coding.ResolveProjectTrusted(coding.ResolveProjectTrustedOptions{
				Cwd:                 cwd,
				TrustStore:          coding.NewProjectTrustStore(agentDir),
				TrustOverride:       trustOverride,
				DefaultProjectTrust: settings.GetDefaultProjectTrust(),
			})
			if err != nil {
				return nil, err
			}
			settings.RebindProject(cwd, trusted)
		}
		options := *base
		options.Cwd = cwd
		options.SessionManager = manager
		if recomputeScope != nil {
			scoped, diagnostics, scopeErr := recomputeScope(ctx)
			if scopeErr != nil {
				return nil, scopeErr
			}
			options.ScopedModels = scoped
			for _, diagnostic := range diagnostics {
				if reported[diagnostic.Message] {
					continue
				}
				reported[diagnostic.Message] = true
				_, _ = fmt.Fprintln(stderr, coding.FormatCLIDiagnostic(coding.CLIDiagnostic{
					Type: diagnostic.Type, Message: diagnostic.Message,
				}))
			}
		}
		created, err := coding.CreateAgentSession(ctx, &options)
		if err != nil {
			return nil, err
		}
		setMCPSession(created.Session)
		return created.Session, nil
	}
}
