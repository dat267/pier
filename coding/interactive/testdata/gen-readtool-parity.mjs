// Reference: pi v1.5.2 (b4afdddf), tool-execution.ts and tools/renderers/read.ts.
// Renders the read tool's call and result rows for the shared corpus.
import { component, corpus, entries, emit, upstream, ui, tui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await component("tool-execution");
const { readRenderers } = await upstream("coding-agent/src/core/tools/renderers/read.ts");
const { KeybindingsManager } = await upstream("coding-agent/src/core/keybindings.ts");
tui.setKeybindings(new KeybindingsManager());
const c = corpus("readtool");
for (const [label, s] of entries(c.cases)) {
	const v = new ToolExecutionComponent(
		"read",
		"call-1",
		s.args,
		{ showImages: s.showImages ?? true },
		readRenderers,
		ui,
		c.cwd,
	);
	if (s.expanded) v.setExpanded(true);
	v.setArgsComplete();
	v.updateResult(s.result, s.isPartial ?? false);
	emit(`x ${label}@${s.width}`, v.render(s.width));
}
