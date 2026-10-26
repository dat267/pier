// Reference: pi v1.4.0 (4a080252), tree-selector.ts and SessionManager.getTree.
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { component, upstream, corpus, entries, emit, tui } from "./golden-reference.mjs";
const { TreeSelectorComponent } = await component("tree-selector");
const { SessionManager } = await upstream("coding-agent/src/core/session-manager.ts");
const { KeybindingsManager } = await upstream("coding-agent/src/core/keybindings.ts");
tui.setKeybindings(new KeybindingsManager());
const c = corpus("tree");
const dir = mkdtempSync(join(tmpdir(), "pier-tree-golden-"));
try {
	const path = join(dir, "session.jsonl");
	writeFileSync(path, c.sessionJsonl);
	const now = Date.parse("2026-05-01T12:00:00Z");
	const RealDate = Date;
	globalThis.Date = class extends RealDate {
		constructor(...args) {
			super(...(args.length ? args : [now]));
		}
		static now() {
			return now;
		}
	};
	const project = (tree) =>
		tree.map((n) => ({
			id: n.entry.id,
			parentId: n.entry.parentId ?? null,
			type: n.entry.type,
			label: n.label ?? null,
			labelTimestamp: n.labelTimestamp ?? null,
			children: project(n.children),
		}));
	for (const [label, s] of entries(c.cases)) {
		const tree = SessionManager.open(path).getTree();
		emit(`x ${label}-tree`, project(tree));
		const v = new TreeSelectorComponent(
			tree,
			s.currentLeafId ?? null,
			s.terminalHeight || 40,
			() => {},
			() => {},
			undefined,
			s.initialSelectedId,
			s.initialFilterMode,
		);
		emit(`x ${label}@${s.width}`, v.render(s.width));
		for (const [i, input] of (s.inputs ?? []).entries()) {
			v.handleInput(input);
			emit(`x ${label}+${i}+${JSON.stringify(input)}@${s.width}`, v.render(s.width));
		}
	}
} finally {
	rmSync(dir, { recursive: true, force: true });
}
