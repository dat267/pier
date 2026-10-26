// Reference: pi v1.4.0 (4a080252), session-selector.ts.
import { component, upstream, corpus, entries, emit, tui } from "./golden-reference.mjs";
const { SessionSelectorComponent } = await component("session-selector");
const { KeybindingsManager } = await upstream("coding-agent/src/core/keybindings.ts");
const keys = new KeybindingsManager();
tui.setKeybindings(keys);
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
const flush = () => new Promise((resolve) => setImmediate(resolve));
const infos = (specs) =>
	specs.map((s) => ({ ...s, created: new Date(now - s.modifiedAgoMs), modified: new Date(now - s.modifiedAgoMs) }));
for (const [label, s] of entries(corpus("session").cases)) {
	const events = [],
		renames = [];
	const current = infos(s.currentSessions ?? []),
		all = s.allSessions ? infos(s.allSessions) : current;
	const currentLoader = async (onProgress) => {
		if (s.loadError) throw new Error(s.loadError);
		if (s.progress?.length === 2) onProgress(...s.progress);
		return current;
	};
	const allLoader = async () => {
		if (s.loadError) throw new Error(s.loadError);
		return all;
	};
	const options = {
		keybindings: keys,
		showRenameHint: s.showRenameHint,
		renameSession:
			s.canRename === false
				? undefined
				: async (p, n) => {
						renames.push(p + "=" + n);
					},
	};
	const v = new SessionSelectorComponent(
		currentLoader,
		allLoader,
		(p) => events.push("select:" + p),
		() => events.push("cancel"),
		() => events.push("exit"),
		() => {},
		options,
		s.currentSessionFilePath,
	);
	await flush();
	emit(`x ${label}@${s.width}`, v.render(s.width));
	for (const [i, input] of (s.inputs ?? []).entries()) {
		v.handleInput(input);
		await flush();
		emit(`x ${label}+${i}+${JSON.stringify(input)}@${s.width}`, v.render(s.width));
	}
	emit(`x ${label}-events`, events);
	emit(`x ${label}-renames`, renames);
	v.header.setStatusMessage(null);
}
