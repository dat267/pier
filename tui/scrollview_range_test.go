package tui

import "testing"

// pi v1.4.0 packages/tui/src/components/scroll-view.ts requests a render after
// a changed scrollTo state, even when disableFollow changes only follow state.
func TestScrollToDisableFollowAtCurrentEndRequestsRender(t *testing.T) {
	view := NewScrollView(NewText("x", 0, 0, nil), ScrollViewOptions{Follow: "end"})
	requests := 0
	view.UpdateLayout(100, 10, func() { requests++ })
	requests = 0

	view.ScrollTo(90, ScrollToRequest{DisableFollow: true})
	if view.IsFollowingEnd() {
		t.Fatal("scrollTo did not disable follow-end")
	}
	if view.ScrollTop() != 90 {
		t.Fatalf("scrollTop = %d, want 90", view.ScrollTop())
	}
	if requests != 1 {
		t.Fatalf("render requests = %d, want 1 for the follow-state change", requests)
	}
}

// GUI scrollToStart is proportional to the laid-out range it has:
//
//	const changed = this.currentScrollTop !== 0 ||
//		this.followingEnd !== (this.followEnd && this.contentHeight <= this.currentViewportHeight);
//	this.followingEnd = this.followEnd && this.contentHeight <= this.currentViewportHeight;
//
// pi v1.4.0 (4a080252), packages/tui/src/components/scroll-view.ts scrollToStart
// and updateLayout. Before the first layout (or while the content still fits)
// there is no range, so scrolling to the start is a no-op that keeps following
// the end; the following layout then pins the view to the new end. CR-166 was a
// flow-test precondition: it scrolled before the range existed and then waited
// for a state that CANNOT change until a later layout.
func TestScrollToStartWithoutARangeKeepsFollowingTheEnd(t *testing.T) {
	view := NewScrollView(NewText("x", 0, 0, nil), ScrollViewOptions{Follow: "end"})
	view.ScrollToStart()
	if !view.IsFollowingEnd() || view.ScrollTop() != 0 {
		t.Fatalf("no-range scroll-to-start: following=%v top=%d; want following at 0", view.IsFollowingEnd(), view.ScrollTop())
	}
	view.UpdateLayout(100, 10, func() {})
	if !view.IsFollowingEnd() || view.ScrollTop() != 90 {
		t.Fatalf("layout after no-range scroll-to-start: following=%v top=%d; want end at 90", view.IsFollowingEnd(), view.ScrollTop())
	}

	// The control: with a range, the same call leaves follow-end.
	control := NewScrollView(NewText("x", 0, 0, nil), ScrollViewOptions{Follow: "end"})
	control.UpdateLayout(100, 10, func() {})
	control.ScrollToStart()
	if control.IsFollowingEnd() || control.ScrollTop() != 0 {
		t.Fatalf("ranged scroll-to-start: following=%v top=%d; want not following at 0", control.IsFollowingEnd(), control.ScrollTop())
	}
}
