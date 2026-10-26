// Reference: pi v1.4.0 (4a080252), tool-execution.ts and tools/renderers/edit.ts.
// Final render of the historical edit fixture, without touching its fixed path.
import { component, upstream, ui } from "./golden-reference.mjs";
const { ToolExecutionComponent } = await component("tool-execution");
const { editRenderers } = await upstream("coding-agent/src/core/tools/renderers/edit.ts");
const { generateDiffString } = await upstream("coding-agent/src/core/tools/edit-diff.ts");
const path = "/tmp/pier_parity/f.txt";
const before = "line 1: keep\nline 2: old value\nline 3: keep\nline 4: old value again\n";
const after = "line 1: multi\nline 2: old value\nline 3: multi too\nline 4: old value again\n";
const v = new ToolExecutionComponent(
	"edit",
	"call-1",
	{
		path,
		edits: [
			{ oldText: "line 1: keep", newText: "line 1: multi" },
			{ oldText: "line 3: keep", newText: "line 3: multi too" },
		],
	},
	{},
	{ renderShell: "self", ...editRenderers },
	ui,
	"/tmp/pier_parity",
);
const preview = generateDiffString(before, after, 3);
v.rendererState.callComponent.preview = preview;
v.setArgsComplete();
v.updateResult(
	{
		content: [{ type: "text", text: "Edited " + path }],
		details: { diff: preview.diff, patch: "", firstChangedLine: 1 },
	},
	false,
);
for (const line of v.render(80)) console.log(JSON.stringify(line));
