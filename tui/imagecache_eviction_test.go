package tui

import (
	"encoding/base64"
	"fmt"
	"testing"
)

// pi v1.4.0 (4a080252), components/image.ts toPng keeps successes and
// failures in the same 32-entry cache. Recreated image components must retry
// an evicted failure rather than retaining every failed source indefinitely.
func TestImageTranscodeCacheBoundsFailedConversions(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	t.Cleanup(func() { SetImageTranscoder(nil) })
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprintf("success=%t", success), func(t *testing.T) {
			calls := 0
			SetImageTranscoder(func(string, string) (string, bool) {
				calls++
				return "iVBORw0KGgoAAAANSUhEUgAAACgAAAAK", success
			})
			render := func(index int) {
				data := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("input-%d", index)))
				NewImageComponent(data, "image/jpeg", ImageTheme{}, ImageOptions{}, &ImageDimensions{WidthPx: 1, HeightPx: 1}).Render(20)
			}
			for index := range 33 {
				render(index)
			}
			if calls != 33 {
				t.Fatalf("initial conversion calls = %d, want 33", calls)
			}
			render(0)
			if calls != 34 {
				t.Fatalf("conversion calls after revisiting evicted source = %d, want 34", calls)
			}
		})
	}
}
