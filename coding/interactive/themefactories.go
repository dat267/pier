package interactive

import (
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// Port of the theme-derived factories from
// src/modes/interactive/theme/theme.ts (getMarkdownTheme, getSelectListTheme,
// getEditorTheme, getSettingsListTheme, getResolvedThemeColors,
// isLightTheme, getLanguageFromPath, highlightCode). Syntax highlighting is
// built in through the `highlight` port; D231 records the wire-in.

// activeTheme is the stable theme handle ActiveTheme returns. It mirrors
// upstream's `theme` Proxy: its methods resolve the current concrete theme on
// every call, so a theme switch reaches components that captured the handle at
// construction (a rebuild would be needed otherwise). It is created once and
// never installed as the current theme (setGlobalTheme always stores concrete
// themes). D166: this stable handle replaces the Proxy.
var activeTheme = &Theme{resolve: CurrentTheme}

// ActiveTheme returns the active theme; it panics when no theme is
// initialized (upstream's Proxy throws). Named ActiveTheme because Theme is
// the type (divergence D85).
func ActiveTheme() *Theme {
	if CurrentTheme() == nil {
		panic("Theme not initialized. Call InitTheme() first.")
	}
	return activeTheme
}

// GetMarkdownTheme builds the markdown theme from the active theme.
func GetMarkdownTheme() tui.MarkdownTheme {
	theme := ActiveTheme()
	return tui.MarkdownTheme{
		Heading:         func(text string) string { return theme.Fg("mdHeading", text) },
		Link:            func(text string) string { return theme.Fg("mdLink", text) },
		LinkURL:         func(text string) string { return theme.Fg("mdLinkUrl", text) },
		Code:            func(text string) string { return theme.Fg("mdCode", text) },
		CodeBlock:       func(text string) string { return theme.Fg("mdCodeBlock", text) },
		CodeBlockBorder: func(text string) string { return theme.Fg("mdCodeBlockBorder", text) },
		Quote:           func(text string) string { return theme.Fg("mdQuote", text) },
		QuoteBorder:     func(text string) string { return theme.Fg("mdQuoteBorder", text) },
		Hr:              func(text string) string { return theme.Fg("mdHr", text) },
		ListBullet:      func(text string) string { return theme.Fg("mdListBullet", text) },
		Bold:            theme.Bold,
		Italic:          theme.Italic,
		Underline:       theme.Underline,
		Strikethrough:   theme.Strikethrough,
		HighlightCode: func(code string, lang string) []string {
			return HighlightCode(code, lang)
		},
	}
}

// GetSelectListTheme builds the select-list theme.
func GetSelectListTheme() tui.SelectListTheme {
	theme := ActiveTheme()
	return tui.SelectListTheme{
		SelectedPrefix: func(text string) string { return theme.Fg("accent", text) },
		SelectedText:   func(text string) string { return theme.Fg("accent", text) },
		Description:    func(text string) string { return theme.Fg("muted", text) },
		ScrollInfo:     func(text string) string { return theme.Fg("muted", text) },
		NoMatch:        func(text string) string { return theme.Fg("muted", text) },
	}
}

// GetEditorTheme builds the editor theme.
func GetEditorTheme() tui.EditorTheme {
	theme := ActiveTheme()
	return tui.EditorTheme{
		BorderColor: func(text string) string { return theme.Fg("borderMuted", text) },
		SelectList:  GetSelectListTheme(),
	}
}

// GetSettingsListTheme builds the settings-list theme.
func GetSettingsListTheme() tui.SettingsListTheme {
	theme := ActiveTheme()
	return tui.SettingsListTheme{
		Label: func(text string, selected bool) string {
			if selected {
				return theme.Fg("accent", text)
			}
			return text
		},
		Value: func(text string, selected bool) string {
			if selected {
				return theme.Fg("accent", text)
			}
			return theme.Fg("muted", text)
		},
		Description: func(text string) string { return theme.Fg("dim", text) },
		Cursor:      theme.Fg("accent", "→ "),
		Hint:        func(text string) string { return theme.Fg("dim", text) },
	}
}

// GetResolvedThemeColors returns CSS-compatible hex colors for HTML export.
func GetResolvedThemeColors(themeName string) (map[string]string, error) {
	name := themeName
	if name == "" {
		name = CurrentThemeName()
	}
	if name == "" {
		// Upstream: `themeName ?? currentThemeName ?? SYSTEM_THEME_NAME`.
		name = SystemThemeName
	}
	// Upstream maps loadTheme(name).colors through colorToHex, so an oklch() or
	// indexed color is exported as its resolved hex, not as written.
	theme, err := loadTheme(name, "")
	if err != nil {
		return nil, err
	}
	colors := theme.Colors()
	cssColors := make(map[string]string, len(colors))
	for key, value := range colors {
		cssColors[key] = tui.ColorToHex(value)
	}
	return cssColors, nil
}

// IsLightTheme reports whether a theme name is the light theme.
func IsLightTheme(themeName string) bool { return themeName == "light" }

// GetThemeExportColors returns the explicit export colors of a theme
// (upstream getThemeExportColors: var references resolved, palette indexes
// converted to hex).
func GetThemeExportColors(themeName string) (pageBg string, cardBg string, infoBg string, err error) {
	name := themeName
	if name == "" {
		name = CurrentThemeName()
	}
	if name == "" {
		name = SystemThemeName
	}
	if name == SystemThemeName {
		// Upstream returns {} for the system theme (it has no export section).
		return "", "", "", nil
	}
	themeJSON, themeErr := loadThemeJSON(name)
	if themeErr != nil {
		return "", "", "", themeErr
	}
	if themeJSON.Export == nil {
		return "", "", "", nil
	}
	colorValue := func(value *ColorValue) string {
		if value == nil {
			return ""
		}
		resolved := resolveVarRefs(*value, themeJSON.Vars, map[string]bool{})
		if resolved.IsIndex {
			return ansi256ToHex(resolved.Index)
		}
		if resolved.Value == "" {
			return ""
		}
		// CSS understands hex and oklch() but not okhsl(); convert it.
		if strings.HasPrefix(strings.ToLower(resolved.Value), "okhsl(") {
			if color, err := tui.ParseColor(resolved.Value); err == nil {
				return tui.ColorToHex(color)
			}
		}
		return resolved.Value
	}
	return colorValue(themeJSON.Export.PageBg), colorValue(themeJSON.Export.CardBg), colorValue(themeJSON.Export.InfoBg), nil
}

// GetLanguageFromPath maps a file extension to a language identifier.
func GetLanguageFromPath(filePath string) (string, bool) {
	// Upstream is `filePath.split(".").pop()?.toLowerCase()`: with no dot the
	// whole (lowercased) path is the lookup key, which is how bare "Makefile"
	// and "Dockerfile" resolve.
	ext := filePath
	if index := strings.LastIndex(filePath, "."); index >= 0 {
		ext = filePath[index+1:]
	}
	if ext == "" {
		return "", false
	}
	language, ok := extensionToLanguage[ai.JSLower(ext)]
	return language, ok
}

var extensionToLanguage = map[string]string{
	"ts": "typescript", "tsx": "typescript", "js": "javascript", "jsx": "javascript",
	"mjs": "javascript", "cjs": "javascript", "py": "python", "rb": "ruby", "rs": "rust",
	"go": "go", "java": "java", "kt": "kotlin", "swift": "swift", "c": "c", "h": "c",
	"cpp": "cpp", "cc": "cpp", "cxx": "cpp", "hpp": "cpp", "cs": "csharp", "php": "php",
	"sh": "bash", "bash": "bash", "zsh": "bash", "fish": "fish", "ps1": "powershell",
	"sql": "sql", "html": "html", "htm": "html", "css": "css", "scss": "scss", "sass": "sass",
	"less": "less", "json": "json", "yaml": "yaml", "yml": "yaml", "toml": "toml", "xml": "xml",
	"md": "markdown", "markdown": "markdown", "dockerfile": "dockerfile", "makefile": "makefile",
	"cmake": "cmake", "lua": "lua", "perl": "perl", "r": "r", "scala": "scala", "clj": "clojure",
	"ex": "elixir", "exs": "elixir", "erl": "erlang", "hs": "haskell", "ml": "ocaml", "vim": "vim",
	"graphql": "graphql", "proto": "protobuf", "tf": "hcl", "hcl": "hcl",
}
