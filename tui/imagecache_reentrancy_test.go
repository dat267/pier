package tui

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), components/image.ts toPng invokes the configured
// callback without a lock; setImageTranscoder may replace it and clear cached
// results. In Go a converter must not retain the cache mutex across user code.
func TestImageTranscoderCanReplaceRegistrationDuringConversion(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	t.Cleanup(func() { SetImageTranscoder(nil) })
	changed := make(chan struct{})
	blocked := false
	newCalls := 0
	SetImageTranscoder(func(string, string) (string, bool) {
		go func() {
			SetImageTranscoder(func(string, string) (string, bool) {
				newCalls++
				return "iVBORw0KGgoAAAANSUhEUgAAACgAAAAK", true
			})
			close(changed)
		}()
		// The timeout releases a buggy converter's retained lock so the
		// regression fails cleanly rather than leaving global state deadlocked.
		select {
		case <-changed:
		case <-time.After(time.Second):
			blocked = true
		}
		return "", false
	})
	render := func() {
		NewImageComponent("anBlZw==", "image/jpeg", ImageTheme{}, ImageOptions{}, &ImageDimensions{WidthPx: 1, HeightPx: 1}).Render(20)
	}
	render()
	<-changed
	if blocked {
		t.Error("converter held the cache lock while waiting for replacement registration")
	}
	// A conversion from before the registration change must not repopulate
	// the replacement converter's freshly cleared cache with its old failure.
	render()
	if newCalls != 1 {
		t.Errorf("replacement converter calls = %d, want 1", newCalls)
	}
}

// Unlocked callbacks can finish the same source concurrently. Publishing both
// must not consume two eviction slots for one source (upstream Map.set dedups).
func TestConcurrentImageConversionsUseOneCacheSlot(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	t.Cleanup(func() { SetImageTranscoder(nil) })
	started, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int32
	SetImageTranscoder(func(data, _ string) (string, bool) {
		calls.Add(1)
		if data == "same" {
			started <- struct{}{}
			<-release
		}
		return "iVBORw0KGgoAAAANSUhEUgAAACgAAAAK", true
	})
	render := func(data string) {
		NewImageComponent(data, "image/jpeg", ImageTheme{}, ImageOptions{}, &ImageDimensions{WidthPx: 1, HeightPx: 1}).Render(20)
	}
	done := make(chan struct{}, 2)
	for range 2 {
		go func() { render("same"); done <- struct{}{} }()
	}
	<-started
	<-started
	close(release)
	<-done
	<-done
	for index := range 31 {
		render("other-" + strconv.Itoa(index))
	}
	render("same")
	if calls.Load() != 33 {
		t.Fatalf("conversion calls = %d, want 33 with the shared source still cached", calls.Load())
	}
}
