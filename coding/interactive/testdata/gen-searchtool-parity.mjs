// Reference: pi v1.5.2 (b4afdddf), tool-execution.ts and tools/renderers/{grep,find,ls}.ts.
// Renders the search tools' call and result rows for the shared corpus.
import { component, corpus, entries, emit, upstream, ui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await component("tool-execution");
const renderers = {
	grep: (await upstream("coding-agent/src/core/tools/renderers/grep.ts")).grepRenderers,
	find: (await upstream("coding-agent/src/core/tools/renderers/find.ts")).findRenderers,
	ls: (await upstream("coding-agent/src/core/tools/renderers/ls.ts")).lsRenderers,
};
const c = corpus("searchtool");
for (const [label, s] of entries(c.cases)) {
	const v = new ToolExecutionComponent(s.tool, "call-1", s.args, {}, renderers[s.tool], ui, c.cwd);
	if (s.expanded) v.setExpanded(true);
	v.setArgsComplete();
	v.updateResult(s.result, s.isPartial ?? false);
	emit(`x ${label}@${s.width}`, v.render(s.width));
}
