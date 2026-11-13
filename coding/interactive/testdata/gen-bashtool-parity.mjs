// Reference: pi v1.5.2 (b4afdddf), tool-execution.ts and tools/renderers/bash.ts.
// Renders the shell tool's call and result rows for the shared corpus.
import { component, corpus, entries, emit, upstream, ui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await component("tool-execution");
const { createShellRenderers } = await upstream("coding-agent/src/core/tools/renderers/bash.ts");
const c = corpus("bashtool");
for (const [label, s] of entries(c.cases)) {
	const definition = { ...createShellRenderers("$") };
	const v = new ToolExecutionComponent(
		"bash",
		"call-1",
		s.args,
		{ showImages: s.showImages ?? true },
		definition,
		ui,
		c.cwd,
	);
	if (s.expanded) v.setExpanded(true);
	v.setArgsComplete();
	v.updateResult(s.result, s.isPartial ?? false);
	emit(`x ${label}@${s.width}`, v.render(s.width));
}
