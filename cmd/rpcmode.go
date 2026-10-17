package cmd

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/coding/interactive"
)

// runRPCSession follows pi v1.3.0 rpc-mode.ts signal cleanup. Return the
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
	done := make(chan struct{})
	code := 0
	go func() {
		defer close(done)
		select {
		case sig := <-signals:
			code = 143
			if sig == syscall.SIGHUP {
				code = 129
			}
			cancel()
		case <-ctx.Done():
		}
	}()
	err := coding.RunRPCMode(ctx, session, coding.RPCModeOptions{
		Input: os.Stdin, Output: os.Stdout, ReplaceSession: replace,
		ExportToHtml: func(exported *coding.AgentSession, outputPath string) (string, error) {
			return interactive.ExportSessionToHTML(exported, outputPath, themeName)
		},
	})
	signal.Stop(signals)
	cancel()
	<-done
	if code != 0 {
		return reportedExitCode(code)
	}
	return err
}

// rpcSessionReplacer builds the session factory for the RPC session-replacement
// commands (upstream AgentSessionRuntime.createRuntime). The new session shares
// the model runtime, settings and tool selection; the cwd and session manager
// come from the target. A cross-project switch resolves that project's trust
// without a prompt (upstream reports hasUI false) and rebinds the one settings
// manager (D160).
func rpcSessionReplacer(base *coding.CreateAgentSessionOptions, settings *coding.SettingsManager, agentDir string, trustOverride *bool, setMCPSession func(*coding.AgentSession)) func(context.Context, *coding.SessionManager) (*coding.AgentSession, error) {
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
		created, err := coding.CreateAgentSession(ctx, &options)
		if err != nil {
			return nil, err
		}
		setMCPSession(created.Session)
		return created.Session, nil
	}
}
