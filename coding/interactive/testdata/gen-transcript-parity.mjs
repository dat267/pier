// Reference: pi v1.5.2 (b4afdddf), core/session-manager.ts
// sessionEntryToContextMessages and buildContextEntries: the entries a transcript
// turns into messages, and the compaction-aware list it renders.
import { corpus, entries, emit, upstream } from "./golden-reference.mjs";
const { sessionEntryToContextMessages, buildContextEntries } = await upstream(
	"coding-agent/src/core/session-manager.ts",
);
const c = corpus("transcript");
for (const [label, s] of entries(c.cases)) {
	const byId = new Map(s.entries.map((entry) => [entry.id, entry]));
	const messages = {};
	for (const id of s.messages ?? []) {
		messages[id] = sessionEntryToContextMessages(byId.get(id));
	}
	emit(`x ${label}-messages`, messages);
	const context = buildContextEntries(s.entries, s.leafId ?? undefined, byId);
	emit(`x ${label}-context`, context.map((entry) => entry.id));
}
