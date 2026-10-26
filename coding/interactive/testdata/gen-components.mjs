// node --experimental-strip-types coding/interactive/testdata/gen-components.mjs ./pi
// Reference: pi v1.4.0 (4a080252), interactive/components/*.ts, with pier's palette.
import { component, corpus, emit, tui, theme, ui } from "./golden-reference.mjs";
const c = corpus("components");
const hints = await component("keybinding-hints");
const { DynamicBorder } = await component("dynamic-border");
const { truncateToVisualLines } = await component("visual-truncate");
const { UserMessageComponent } = await component("user-message");
const { CustomEntryComponent } = await component("custom-entry");
const { BorderedLoader } = await component("bordered-loader");
for (const key of c.keybindings)
	emit("c keytext " + key, [hints.keyText(key), hints.keyDisplayText(key), hints.keyHint(key, "description")]);
for (const key of c.rawKeys)
	emit("c rawkey " + key, [
		hints.formatKeyText(key),
		hints.formatKeyText(key, { capitalize: true }),
		hints.rawKeyHint(key, "hint"),
	]);
for (const w of c.borderWidths) {
	emit("c border@" + w, new DynamicBorder().render(w));
	emit("c border-red@" + w, new DynamicBorder((s) => "<" + s + ">").render(w));
}
for (const [i, s] of c.truncate.entries()) {
	const r = truncateToVisualLines(s.text, s.max, s.width, s.pad ?? 0);
	emit("c truncate" + i, [r.visualLines, r.skippedCount]);
}
for (const [i, s] of c.userMessages.entries())
	emit(`c user${i}@${s.width}`, new UserMessageComponent(s.text, undefined, s.pad ?? 1).render(s.width));
emit("c user-single-copy@30", new UserMessageComponent("hello **bold** world").render(30));
for (const [i, s] of c.customEntries.entries()) {
	const v = new CustomEntryComponent(
		{ customType: s.type, data: s.data },
		(entry, o) => new tui.Text(`custom:${entry.customType}:${o.expanded ? "expanded" : "collapsed"}`, 0, 0),
	);
	emit(`c custom${i}-collapsed@${s.width}`, v.render(s.width));
	emit(`c custom${i}-hasContent`, v.hasContent());
	v.setExpanded(true);
	emit(`c custom${i}-expanded@${s.width}`, v.render(s.width));
}
const broken = new CustomEntryComponent({ customType: "broken" }, () => {
	throw new Error("boom");
});
emit("c custom-failed", broken.render(30));
const empty = new CustomEntryComponent({ customType: "empty" }, () => undefined);
emit("c custom-empty", [empty.hasContent(), empty.render(20)]);
for (const [label, message, options] of [
	["loader", "Working...", undefined],
	["loader-plain", "Plain", { cancellable: false }],
]) {
	const v = new BorderedLoader(ui, theme, message, options);
	v.dispose();
	emit(`c ${label}@30`, v.render(30));
}
