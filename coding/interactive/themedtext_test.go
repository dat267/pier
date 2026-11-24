package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Port of the ThemedText case in upstream themed-text.test.ts (bf8e4b953):
// content is built lazily and rebuilt with the current theme after invalidation.
func TestThemedTextRebuildsOnInvalidate(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	builds := 0
	text := NewThemedText(func() string {
		builds++
		return ActiveTheme().Fg("accent", "hello")
	}, 1, 1)
	if builds != 0 {
		t.Fatalf("built eagerly (%d builds)", builds)
	}
	dark := strings.Join(text.Render(20), "")

	InitTheme("light", false)
	if got := strings.Join(text.Render(20), ""); got != dark {
		t.Fatalf("recolored without an invalidate")
	}
	text.Invalidate()
	light := strings.Join(text.Render(20), "")
	if light == dark {
		t.Fatal("did not recolor after invalidate")
	}
	if !strings.Contains(light, ActiveTheme().GetFgAnsi("accent")) {
		t.Fatalf("rebuilt text used a stale accent: %q", light)
	}
	if builds != 2 {
		t.Fatalf("builds = %d (want 2)", builds)
	}
}

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

// pi v1.5.2 (b4afdddf), modes/interactive/interactive-mode.ts:4285
// (addCacheWarmingUsage): a warm that lands on the trailing warming line with
// the same note adds to that line's total instead of printing another one, and
// a message in between starts a fresh line.
func TestCacheWarmingNoticeTotalsConsecutiveWarms(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, false)
	settings.SetShowCacheMissNotices(true)
	warm := func(total float64, note string) *coding.SessionEntry {
		entry := &coding.SessionEntry{Type: "usage", Kind: "cache_warm", Usage: &ai.Usage{Cost: ai.UsageCost{Total: total}}}
		if note != "" {
			value := note
			entry.Note = &value
		}
		return entry
	}
	warmingLines := func() []string {
		var found []string
		for _, line := range renderChat(t, renderer.Chat) {
			if strings.Contains(coding.StripAnsi(line), "Cache warmed") {
				found = append(found, strings.TrimSpace(coding.StripAnsi(line)))
			}
		}
		return found
	}

	renderer.AddCacheWarmingUsage(warm(0.001, ""))
	// Paint the first line: the collapse has to repaint the text it totals into.
	renderChat(t, renderer.Chat)
	renderer.AddCacheWarmingUsage(warm(0.002, ""))
	if lines := warmingLines(); len(lines) != 1 || !strings.Contains(lines[0], "$0.003") {
		t.Fatalf("consecutive warms = %q, want one line totalling $0.003", lines)
	}

	// A rendered message breaks the run of warms; the next pair totals again.
	renderer.Chat.AddChild(tui.NewText("between", 0, 0, nil))
	renderer.AddCacheWarmingUsage(warm(0.001, "extension override"))
	renderer.AddCacheWarmingUsage(warm(0.002, "extension override"))
	lines := warmingLines()
	if len(lines) != 2 {
		t.Fatalf("warms after a message = %q, want two lines", lines)
	}
	if !strings.Contains(lines[0], "$0.003") || !strings.Contains(lines[1], "(extension override): $0.003") {
		t.Fatalf("warming lines = %q, want $0.003 then the override total", lines)
	}

	// A different note is a different line even with nothing in between.
	renderer.AddCacheWarmingUsage(warm(0.004, ""))
	lines = warmingLines()
	if len(lines) != 3 || !strings.Contains(lines[2], "$0.004") {
		t.Fatalf("warming lines = %q, want an unnotted line after the override total", lines)
	}
}
