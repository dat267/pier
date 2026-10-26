// Reference: pi v1.4.0 (4a080252), components/tool-execution.ts.
import { component, legacyComponent, corpus, entries, emit, tui, ui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await legacyComponent("tool-execution");
for (const [label, s] of entries(corpus("exec").tool)) {
	let definition;
	if (!s.noDefinition) {
		definition = {
			renderShell: s.selfShell ? "self" : undefined,
			renderCall:
				s.callRenderer === "throw"
					? () => {
							throw new Error("call boom");
						}
					: (args) => new tui.Text("call:" + JSON.stringify(args), 0, 0),
			renderResult:
				s.resultRenderer === "none"
					? undefined
					: s.resultRenderer === "throw"
						? () => {
								throw new Error("result boom");
							}
						: (r, o) =>
								new tui.Text(
									`result:${o.expanded ? "expanded" : "collapsed"}:${o.isPartial ? "partial" : "final"}:${r.content
										.filter((v) => v.type === "text")
										.map((v) => v.text)
										.join("|")}`,
									0,
									0,
								),
		};
	}
	const v = new ToolExecutionComponent(s.name, "call-1", s.args, { showImages: true }, definition, ui, "/tmp");
	if (s.executionStarted) v.markExecutionStarted();
	if (s.argsComplete) v.setArgsComplete();
	if (s.result) v.updateResult(s.result, s.isPartial ?? false);
	if (s.expanded) v.setExpanded(true);
	emit(`e tool ${label}@${s.width}`, v.render(s.width));
	if (s.expandAfter) {
		v.setExpanded(true);
		emit(`e tool ${label}-expanded@${s.width}`, v.render(s.width));
	}
}
