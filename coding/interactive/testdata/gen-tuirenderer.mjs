// Reference: pi v1.4.0 (4a080252), tui-renderer.ts style option closures.
import { component, corpus, emit, theme } from "./golden-reference.mjs";
const { keyDisplayText } = await component("keybinding-hints");
const { text } = corpus("tuirenderer");
const search = (t) => theme.bg("searchMatchBg", theme.fg("searchMatchText", t));
emit("x searchMatchStyle", theme.underline(search(text)));
emit("x searchCurrentMatchStyle", theme.bold(theme.inverse(search(text))));
emit("x searchNavigationButtonStyle", [theme.underline(text), text]);
const shortcut = keyDisplayText("tui.altScreen.bottom");
emit(
	"x scrollToEndIndicator",
	theme.bg("selectedBg", theme.fg("text", ` ↓ Jump to latest message${shortcut ? " · " + shortcut : ""} `)),
);
emit("x shortcut", shortcut);
