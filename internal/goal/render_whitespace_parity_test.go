package goal

import (
	"strings"
	"testing"
)

// D223 reference: installed goal/render.ts normalizes call-card text with JS
// trim and /\s+/. NEXT LINE must remain visible and count toward truncation.
func TestGoalCallCardsPreserveNonJSWhitespace(t *testing.T) {
	text := RenderSetGoalStatusRenderCall("blocked", "\u0085reason\u0085", stubTheme{}, true).Text()
	if want := "Set goal → blocked\n\n\u0085reason\u0085"; text != want {
		t.Errorf("expanded reason=%q, want %q", text, want)
	}
	objective := strings.TrimSpace(strings.Repeat("line\n", 7))
	text = RenderSetGoalRenderCall(objective, stubTheme{}, false).Text()
	if !strings.Contains(text, "(2 more lines") || !strings.Contains(text, "click or expand") {
		t.Errorf("missing line-count expansion hint: %q", text)
	}
}
