package goal

import (
	"strings"
	"testing"
)

// D223 reference: installed goal/render.ts normalizes call-card text with JS
// trim and /\s+/. NEXT LINE must remain visible and count toward truncation.
func TestGoalCallCardsPreserveNonJSWhitespace(t *testing.T) {
	text := RenderSetGoalStatusRenderCall("blocked", "\u0085reason\u0085", stubTheme{}, true).Text()
	if want := "Set goal → blocked: \u0085reason\u0085"; text != want {
		t.Errorf("expanded reason=%q, want %q", text, want)
	}
	text = RenderSetGoalRenderCall("\u0085"+strings.Repeat("x", 59)+"\u0085", stubTheme{}, false).Text()
	if !strings.Contains(text, "click or expand") {
		t.Errorf("missing expansion hint for a 61-unit objective: %q", text)
	}
}
