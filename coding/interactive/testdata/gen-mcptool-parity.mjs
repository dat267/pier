// Reference: pi v1.5.2 (b4afdddf), tool-execution.ts and
// extensions/mcp/tools.ts createMcpToolRenderers.
import { component, corpus, entries, emit, upstream, ui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await component("tool-execution");
const { createMcpToolRenderers } = await upstream("coding-agent/src/extensions/mcp/tools.ts");
const c = corpus("mcptool");
for (const [label, s] of entries(c.cases)) {
	const v = new ToolExecutionComponent(s.label, "call-1", s.args, {}, createMcpToolRenderers(s.label), ui, c.cwd);
	if (s.expanded) v.setExpanded(true);
	v.setArgsComplete();
	v.updateResult(s.result, s.isPartial ?? false);
	emit(`x ${label}@${s.width}`, v.render(s.width));
}
