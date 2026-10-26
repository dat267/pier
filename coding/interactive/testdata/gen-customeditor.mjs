// Reference: pi v1.4.0 (4a080252), custom-editor.ts and status-indicator.ts.
import { component, upstream, corpus, entries, emit, tui, theme, ui } from "./golden-reference.mjs";
const { CustomEditor } = await component("custom-editor");
const { StatusIndicator } = await component("status-indicator");
const { KeybindingsManager } = await upstream("coding-agent/src/core/keybindings.ts");
const keys = new KeybindingsManager();
tui.setKeybindings(keys);
const c = corpus("customeditor");
const editorTheme = { borderColor: (s) => theme.fg("border", s) };
for (const [label, s] of entries(c.render)) {
	const v = new CustomEditor(ui, editorTheme, keys, { embedWorkingStatus: s.embed });
	if (s.indicator != null) {
		const indicator = new StatusIndicator(
			"working",
			ui,
			(s) => theme.fg("accent", s),
			(s) => theme.fg("muted", s),
			s.indicator,
		);
		indicator.dispose();
		v.setWorkingStatusIndicator(indicator);
	}
	v.setText(s.text);
	emit(`x render ${label}@${s.width}/${s.hidden}`, v.renderTopBorder(s.width, s.hidden));
}
for (const [label, s] of entries(c.input)) {
	const events = [];
	const v = new CustomEditor(ui, editorTheme, keys);
	v.setText(s.text);
	v.onEscape = () => events.push("escape");
	v.onCtrlD = () => events.push("ctrlD");
	v.onPasteImage = () => events.push("pasteImage");
	v.onExtensionShortcut = (data) => {
		if (s.extensionShortcut && data === s.extensionShortcut) {
			events.push("ext:" + data);
			return true;
		}
		return false;
	};
	for (const a of s.actions ?? []) v.onAction(a.name, () => events.push("action:" + a.name));
	for (const input of s.inputs ?? []) v.handleInput(input);
	emit("x input " + label, [events, v.getText()]);
}
