package interactive

import (
	"testing"

	"github.com/dat267/pier/coding"
)

// BenchmarkFooterRender measures the footer's per-frame render with extension
// statuses present, the shape the render cache still has to walk.
func BenchmarkFooterRender(b *testing.B) {
	SetCustomThemesDir(b.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	manager := coding.NewSessionManager(b.TempDir(), &coding.SessionManagerOptions{})
	session := &fakeFooterSession{manager: manager}
	provider := coding.NewFooterDataProvider(b.TempDir(), coding.FooterDataProviderOptions{DisableWatch: true})
	b.Cleanup(provider.Dispose)
	for _, key := range []string{"zeta", "alpha", "mu", "beta"} {
		text := key + " status"
		provider.SetExtensionStatus(key, &text)
	}
	footer := NewFooterComponent(session, provider)
	b.ReportAllocs()

	for b.Loop() {
		_ = footer.Render(120)
	}
}
