// Reference: pi v1.4.0 (4a080252), skill/setup/announcement components.
import { component, legacyComponent, corpus, entries, emit } from "./golden-reference.mjs";
const { SkillInvocationMessageComponent } = await component("skill-invocation-message");
const { FirstTimeSetupComponent } = await legacyComponent("first-time-setup");
const { EarendilAnnouncementComponent } = await component("earendil-announcement");
const c = corpus("easter");
for (const [label, s] of entries(c.skill)) {
	const v = new SkillInvocationMessageComponent({ name: s.name, content: s.content });
	emit(`x skill ${label}@${s.width}`, v.render(s.width));
	v.setExpanded(true);
	emit(`x skill ${label}-expanded@${s.width}`, v.render(s.width));
}
for (const [label, s] of entries(c.setup)) {
	const previews = [];
	const v = new FirstTimeSetupComponent({ detectedTheme: s.detected, onThemePreview: (t) => previews.push(t) });
	for (const input of s.inputs ?? []) v.handleInput(input);
	emit(`x setup ${label}@${s.width}`, v.render(s.width));
	emit(`x setup ${label}-previews`, previews);
}
// D94 omits the bundled announcement image, but retains its surrounding spacers.
const announcement = new EarendilAnnouncementComponent();
const removeImages = (container) => {
	if (!container.children) return;
	container.children = container.children.filter((child) => child.constructor.name !== "Image");
	for (const child of container.children) removeImages(child);
};
removeImages(announcement);
emit("x announcement@60", announcement.render(60));
