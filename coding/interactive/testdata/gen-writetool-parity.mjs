// Reference: pi v1.5.2 (b4afdddf), tool-execution.ts and tools/renderers/write.ts.
// Renders the write tool's call and result rows for the shared corpus.
import { component, corpus, entries, emit, upstream, ui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await component("tool-execution");
const { writeRenderers } = await upstream("coding-agent/src/core/tools/renderers/write.ts");
const c = corpus("writetool");
for (const [label, s] of entries(c.cases)) {
	const v = new ToolExecutionComponent("write", "call-1", s.args, {}, writeRenderers, ui, c.cwd);
	if (s.expanded) v.setExpanded(true);
	v.setArgsComplete();
	v.updateResult(s.result, s.isPartial ?? false);
	emit(`x ${label}@${s.width}`, v.render(s.width));
}
