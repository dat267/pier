// Reference: pi v1.4.0 (4a080252), interactive selector components.
import { component, corpus, entries, emit, tui } from "./golden-reference.mjs";
const { ThemeSelectorComponent } = await component("theme-selector");
const { ShowImagesSelectorComponent } = await component("show-images-selector");
const { ThinkingSelectorComponent } = await component("thinking-selector");
const { UserMessageSelectorComponent } = await component("user-message-selector");
const { TrustSelectorComponent } = await component("trust-selector");
const { CustomMessageComponent } = await component("custom-message");
const c = corpus("selector");
const noop = () => {};
for (const [label, s] of entries(c.theme)) {
	const v = new ThemeSelectorComponent(s.current, noop, noop, noop);
	emit(`s theme ${label}@${s.width}`, v.render(s.width));
	emit(`s theme ${label}-selected`, v.getSelectList().getSelectedItem());
}
for (const [label, s] of entries(c.images))
	emit(`s images ${label}@${s.width}`, new ShowImagesSelectorComponent(s.current, noop, noop).render(s.width));
for (const [label, s] of entries(c.thinking)) {
	const v = new ThinkingSelectorComponent(
		s.current,
		["off", "minimal", "low", "medium", "high", "xhigh", "max"],
		noop,
		noop,
		undefined,
		s.default,
	);
	v.focused = true;
	for (const i of s.inputs ?? []) v.handleInput(i);
	emit(`s thinking ${label}@${s.width}`, v.render(s.width));
	emit(`s thinking ${label}-selected`, v.getSelectList().getSelectedItem());
}
for (const [label, s] of entries(c.userMessages)) {
	const v = new UserMessageSelectorComponent(
		s.messages.map((text, i) => ({ id: String(i), text })),
		noop,
		noop,
		s.initial,
	);
	for (const i of s.inputs ?? []) v.getMessageList().handleInput(i);
	emit(`s umsg ${label}@${s.width}`, v.render(s.width));
}
for (const [label, s] of entries(c.trust)) {
	const v = new TrustSelectorComponent({
		cwd: s.cwd,
		savedDecision: s.savedDecision,
		projectTrusted: s.trusted,
		onSelect: noop,
		onCancel: noop,
	});
	for (const i of s.inputs ?? []) v.handleInput(i);
	emit(`s trust ${label}@${s.width}`, v.render(s.width));
}
for (const [label, s] of entries(c.custom)) {
	const renderer =
		s.renderer === "none"
			? undefined
			: s.renderer === "throw"
				? () => {
						throw new Error("boom");
					}
				: (m, o) => new tui.Text(`custom:${m.customType}:${o.expanded ? "e" : "c"}`, 0, 0);
	const v = new CustomMessageComponent({ customType: s.type, content: s.content }, renderer);
	emit(`s custom ${label}@${s.width}`, v.render(s.width));
	v.setExpanded(true);
	emit(`s custom ${label}-expanded@${s.width}`, v.render(s.width));
}
