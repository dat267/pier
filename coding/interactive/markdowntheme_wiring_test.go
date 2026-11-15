package interactive

import (
	"context"
	"testing"

	"github.com/dat267/pier/coding"
)

// Upstream (pi v1.5.2 modes/interactive/interactive-mode.ts
// getMarkdownThemeWithSettings) applies markdown.codeBlockIndent at every render
// site, so the setting reaches the transcript from the first frame and again
// after /reload.
func TestMarkdownThemeAppliesCodeBlockIndent(t *testing.T) {
	indent := "    "
	app, cleanup := newTestAppConfigured(t, false, func(settings *coding.SettingsManager, _ *AppOptions) {
		settings.ApplyOverrides(&coding.Settings{Markdown: &coding.SettingsMarkdown{CodeBlockIndent: &indent}})
	})
	defer cleanup()
	if app.transcript.MarkdownTheme == nil || app.transcript.MarkdownTheme.CodeBlockIndent != indent {
		t.Fatalf("transcript code block indent = %v, want %q", app.transcript.MarkdownTheme, indent)
	}
	if app.events.MarkdownTheme == nil || app.events.MarkdownTheme.CodeBlockIndent != indent {
		t.Fatalf("events code block indent = %v, want %q", app.events.MarkdownTheme, indent)
	}

	// /reload re-reads the setting, as the per-render read does.
	changed := "\t"
	app.settings.ApplyOverrides(&coding.Settings{Markdown: &coding.SettingsMarkdown{CodeBlockIndent: &changed}})
	app.Init(context.Background())
	app.applySettingsDependentUI()
	if got := app.transcript.MarkdownTheme.CodeBlockIndent; got != changed {
		t.Fatalf("code block indent after reload = %q, want %q", got, changed)
	}
	if got := app.events.MarkdownTheme.CodeBlockIndent; got != changed {
		t.Fatalf("events code block indent after reload = %q, want %q", got, changed)
	}
}
