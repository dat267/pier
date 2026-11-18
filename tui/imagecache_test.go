package tui

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
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
