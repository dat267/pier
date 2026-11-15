package interactive

import (
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.5.2 (b4afdddf) components/provider-brand.ts gives Radius a
// logo-color shimmer and OpenRouter a brand-color glow, and oauth-selector.ts
// applies the style to the selected row of a preferred partner provider while
// startBrandAnimation repaints every 50 ms. The port rendered every selected
// name in the accent color with no animation.
func TestOAuthSelectorAnimatesPreferredPartnerNames(t *testing.T) {
	theme, err := loadTheme("dark", ColorModeTruecolor)
	if err != nil {
		t.Fatal(err)
	}
	setGlobalTheme("dark", theme)
	providers := []AuthSelectorProvider{
		{ID: "radius", Name: "Radius", AuthType: "oauth"},
		{ID: "anthropic", Name: "Anthropic", AuthType: "oauth"},
		{ID: "openrouter", Name: "OpenRouter", AuthType: "oauth"},
	}
	component := NewOAuthSelectorComponent("login", providers, nil, nil, "")
	defer component.Dispose()

	if style := getProviderBrandStyle("radius"); style == nil {
		t.Fatal("Radius has no brand style")
	}
	if style := getProviderBrandStyle("openrouter"); style == nil {
		t.Fatal("OpenRouter has no brand style")
	}
	if style := getProviderBrandStyle("anthropic"); style != nil {
		t.Fatal("Anthropic must not have a brand style")
	}

	// The selected Radius row shimmers in the first logo color.
	rendered := strings.Join(component.Render(80), "\n")
	if !strings.Contains(rendered, "\x1b[38;2;77;154;191mR") {
		t.Fatalf("selected Radius name is not shimmered: %q", rendered)
	}
	active, delay := component.AnimationFrame(time.Now())
	if !active || delay != 50*time.Millisecond {
		t.Fatalf("animation = %v, %v", active, delay)
	}

	// The glow breathes and keeps the text.
	first := openRouterGlow("OpenRouter", 0)
	later := openRouterGlow("OpenRouter", 1200*time.Millisecond)
	if first == later {
		t.Fatalf("OpenRouter glow did not advance: %q", first)
	}
	if !strings.Contains(first, "\x1b[38;2;") || !strings.Contains(first, "OpenRouter") {
		t.Fatalf("glow = %q", first)
	}

	// A selected provider without a brand stops the animation.
	component.HandleInput("\x1b[B")
	selected, ok := component.SelectedProvider()
	if !ok || selected.ID != "anthropic" {
		t.Fatalf("selected = %+v", selected)
	}
	if active, _ := component.AnimationFrame(time.Now()); active {
		t.Fatal("an unanimated provider still requests frames")
	}
	rendered = strings.Join(component.Render(80), "\n")
	if strings.Contains(rendered, "\x1b[38;2;77;154;191mR") {
		t.Fatalf("a stale Radius shimmer is still rendered: %q", rendered)
	}

	component.Dispose()
	if active, _ := component.AnimationFrame(time.Now()); active {
		t.Fatal("a disposed selector still animates")
	}
}
