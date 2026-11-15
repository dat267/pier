package interactive

import (
	"testing"

	"github.com/dat267/pier/coding"
)

// Upstream (pi v1.5.2 main.ts) captures autoTrustOnReloadCwd only when no
// --approve/--no-approve override is in force: an override settles trust for
// the run, so a later /reload must not persist it for the project.
func TestTrustOverrideDisablesImplicitReloadTrust(t *testing.T) {
	approved := true
	app, cleanup := newTestAppConfigured(t, false, func(_ *coding.SettingsManager, options *AppOptions) {
		options.ProjectTrustOverride = &approved
	})
	defer cleanup()
	if app.autoTrustOnReloadCwd != "" {
		t.Fatalf("auto trust cwd = %q, want none under a trust override", app.autoTrustOnReloadCwd)
	}

	// Without an override the boot capture stands for a project with no
	// trust-requiring resources.
	control, controlCleanup := newTestApp(t)
	defer controlCleanup()
	if got := control.autoTrustOnReloadCwd; got != control.sessionMgr.GetCwd() {
		t.Fatalf("auto trust cwd = %q, want the session cwd", got)
	}
}
