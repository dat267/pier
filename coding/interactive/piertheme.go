package interactive

// The port's palette (D154) lives only in themes/dark.json and light.json.
// Built-ins, library callers, HTML export and the CLI all load those documents.
// Compared with upstream theme/{dark,light}.json (pi v1.4.0, 4a080252):
//
//   - Decorative selection and search fills use the terminal's background, so
//     transparent or blurred terminals remain visible. Accent prefixes mark
//     selected rows without relying on a filled background.
//   - Tool states and user/custom messages keep distinct signal fills. Without
//     them a failed tool could look identical to a successful one, or a user
//     message could look like assistant text.
//   - Amber accents identify pier; primary text uses the terminal foreground.
//     D219 applies the same accent to the adaptive system theme.

var transparentBackgroundColors = []string{"selectedBg", "searchMatchBg"}

var toolStateBackgroundColors = []string{
	"toolPendingBg", "toolSuccessBg", "toolErrorBg",
}

var filledBackgroundColors = append(append([]string{}, toolStateBackgroundColors...), "userMessageBg", "customMessageBg")

// pierSignalPanelColors shares the built-in signal fills with the system
// theme's no-color fallback (D193), without a second palette definition.
func pierSignalPanelColors(appearance string) map[string]string {
	out := map[string]string{}
	palette, ok := getBuiltinThemes()[appearance]
	if !ok {
		return out
	}
	colors := ResolveThemeColors(palette.Colors, palette.Vars)
	for _, token := range filledBackgroundColors {
		out[token] = colors[token].Value
	}
	return out
}
