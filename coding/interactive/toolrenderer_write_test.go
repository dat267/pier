package interactive

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.2 core/tools/renderers/write.ts clears the streaming cache
// for a complete call and lets the component's CodeHighlighter memo serve
// repeated renders, so an unchanged complete call is highlighted once. The port
// rebuilt the whole cache on every render, which cost 31 ms per frame for a
// 2000-line write.
func TestWriteCallReusesTheHighlightCacheForCompleteArgs(t *testing.T) {
	InitTheme("dark", false)
	theme := ActiveTheme().concrete()
	args := map[string]any{"file_path": "/tmp/proj/a.go", "content": "package a\n"}
	context := &ToolRenderContext{Cwd: "/tmp/proj", ArgsComplete: true}
	component, ok := writeRenderers.RenderCall(args, theme, context).(*writeCallComponent)
	if !ok {
		t.Fatal("render call did not return a write component")
	}
	if component.cache == nil {
		t.Fatal("complete call has no highlight cache")
	}
	cache := component.cache
	context.LastComponent = component
	writeRenderers.RenderCall(args, theme, context)
	if component.cache != cache {
		t.Fatal("unchanged complete call re-highlighted its content")
	}
}

// Reference: pi v1.5.2 theme.ts CodeHighlighter compares the global theme before
// reusing a result, so a theme switch re-highlights. The reuse added for the
// complete-args path must check the theme for the same reason.
func TestWriteCallRebuildsHighlightAfterAThemeChange(t *testing.T) {
	InitTheme("dark", false)
	args := map[string]any{"file_path": "/tmp/proj/a.go", "content": "package a\n"}
	context := &ToolRenderContext{Cwd: "/tmp/proj", ArgsComplete: true}
	component, ok := writeRenderers.RenderCall(args, ActiveTheme().concrete(), context).(*writeCallComponent)
	if !ok {
		t.Fatal("render call did not return a write component")
	}
	cache := component.cache
	InitTheme("light", false)
	context.LastComponent = component
	writeRenderers.RenderCall(args, ActiveTheme().concrete(), context)
	if component.cache == cache {
		t.Fatal("theme change reused highlighting from the previous theme")
	}
}

// BenchmarkWriteCallCompleteArgs renders an unchanged 2000-line complete write
// call through the same component, the way the transcript re-renders it for a
// result delta or an expand toggle.
func BenchmarkWriteCallCompleteArgs(b *testing.B) {
	InitTheme("dark", false)
	var builder strings.Builder
	for i := range 2000 {
		builder.WriteString("func value")
		builder.WriteString(itoa(i))
		builder.WriteString("() int { return ")
		builder.WriteString(itoa(i))
		builder.WriteString(" }\n")
	}
	args := map[string]any{"file_path": "/tmp/proj/big.go", "content": builder.String()}
	theme := ActiveTheme().concrete()
	context := &ToolRenderContext{Cwd: "/tmp/proj", ArgsComplete: true, Expanded: true}
	context.LastComponent = writeRenderers.RenderCall(args, theme, context)

	for b.Loop() {
		args2 := map[string]any{"file_path": args["file_path"], "content": args["content"]}
		context.LastComponent = writeRenderers.RenderCall(args2, theme, context)
	}
}
