package tui

import (
	"encoding/base64"
	"fmt"
	"testing"
)

// pi v1.4.0 (4a080252), components/image.ts toPng refreshes a cache hit
// (delete then set), so the 32-entry bound evicts the least recently used
// source, not the oldest inserted one. A frequently rendered image must not be
// re-converted just because it was inserted first.
func TestImageTranscodeCacheKeepsRecentlyUsedSources(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	t.Cleanup(func() { SetImageTranscoder(nil) })
	calls := map[string]int{}
	SetImageTranscoder(func(data, _ string) (string, bool) {
		calls[data]++
		return "iVBORw0KGgoAAAANSUhEUgAAACgAAAAK", true
	})
	render := func(name string) {
		data := base64.StdEncoding.EncodeToString([]byte(name))
		NewImageComponent(data, "image/jpeg", ImageTheme{}, ImageOptions{}, &ImageDimensions{WidthPx: 1, HeightPx: 1}).Render(20)
	}
	key := func(name string) string { return base64.StdEncoding.EncodeToString([]byte(name)) }
	render("frequent")
	for index := range 31 {
		render(fmt.Sprintf("spare-%d", index))
	}
	// Fill the bound, then touch the first source so it becomes most recent.
	render("frequent")
	if calls[key("frequent")] != 1 {
		t.Fatalf("cache-miss conversions for the frequent source = %d", calls[key("frequent")])
	}
	render("newcomer")
	render("frequent")
	if got := calls[key("frequent")]; got != 1 {
		t.Fatalf("recently used source was re-converted: %d calls, want 1 (oldest inserted entry should be evicted)", got)
	}
}
